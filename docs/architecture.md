# SpellFire Architecture

SpellFire is a playable browser-based multiplayer foundation: a Pixi.js/TypeScript client connects to a server-authoritative Go simulation over binary WebSockets. Account and character metadata is persisted in SQLite. All in-world visuals are built at runtime with Pixi geometry primitives; there are no bitmap assets in the play space.

This document describes what is implemented in version 0.1. [`game/design/`](game/design/README.md) is authoritative for game rules, and [`game/ui/`](game/ui/README.md) is authoritative for player-facing presentation.

## Repository layout

```text
proto/game.proto                    Canonical WebSocket schema
data/tuning/                        Versioned balance tables, read by both server and client
data/data.go                        Embeds the tables into the Go binary
server/cmd/spellfire/               Process entry point and static-client hosting
server/internal/api/                JSON account and character HTTP API
server/internal/auth/               Password hashing, opaque sessions, authentication
server/internal/config/             Environment configuration
server/internal/crafting/           Slotted-blueprint recipes, costs, and component modifiers
server/internal/game/               Fixed-tick world, physics, combat, gunplay, AOI, rewind
server/internal/loadout/            Equipped-slot model, resolution, and validation
server/internal/model/              Persistent domain types
server/internal/protocol/           Protobuf wire codec
server/internal/store/              Persistence interface and SQLite implementation
server/internal/transport/          WebSocket lifecycle and origin enforcement
server/internal/tuning/             Tuning-table schema, loader, and validation
web/src/api.ts                      Typed account/character API client
web/src/tuning.ts                   Client-side view of the same tuning tables
web/src/net/                        Protobuf codec and reconnecting socket
web/src/game/crafting.ts            Client-side view of blueprints, costs, and component behaviour
web/src/game/loadout.ts             Client-side view of the slot model and unlock ledger
web/src/game/prediction.ts          Local prediction and server reconciliation
web/src/game/view.ts                Procedural Pixi renderer and interpolation
web/src/main.ts                     Application state, inputs, HUD, and menus
web/src/style.css                   Responsive UI and touch-safe layout
```

Module dependencies point inward: transport calls authentication, persistence, and the game engine; the world simulation has no HTTP, WebSocket, database, or Pixi dependency. This keeps authoritative rules directly testable.

## Runtime topology

```text
Browser DOM UI ──JSON/HTTP──> account + character API ──> Store interface ──> SQLite
      │
      └─Pixi renderer ──binary Protobuf/WebSocket──> connection handler ──> game engine
                                                               │
                                                     authoritative World
```

The Go process serves the built SPA, API, WebSocket endpoint, and one shared world. SQLite was selected for the initial implementation because it has no external runtime dependency and gives deterministic integration tests. It is isolated behind `store.Store`; a PostgreSQL implementation can replace it without changing account, API, transport, or game logic. Horizontal world sharding is deliberately not claimed by this single-process foundation.

## Application and account flow

- `POST /api/auth/register` validates the email/password, hashes passwords with bcrypt, creates an account, and returns a new opaque session token plus the server-derived account view.
- `POST /api/auth/login` returns the same generic failure for unknown accounts and incorrect passwords, and returns the token plus account view on success.
- `POST /api/auth/logout` revokes only the presented session.
- `GET /api/account` returns the authenticated account email and `is_admin` role.
- `GET|POST /api/characters` lists or creates account-owned characters. Names are 3–20 characters, both GDD classes are accepted, and the initial account limit is four characters.
- `GET /api/version` returns `{time, commit}` describing how the running binary was built. The values are stamped at link time via `-ldflags -X spellfire/server/internal/build.{Time,Commit}` (the `make build` target and the Dockerfile set these; the Docker commit comes from the `BUILD_COMMIT` build arg). For an unstamped local `go build`/`go run` in the checkout, `build.Get` falls back to Go's embedded VCS metadata. The home screen fetches this and shows the build age, so a stale deployment is visible at a glance.
- The raw session token is shown only once to the client. SQLite stores a SHA-256 digest, so a database disclosure does not directly disclose live bearer tokens. The browser keeps the token in `sessionStorage`, preserving refreshes in the current tab without creating an indefinitely persistent browser credential.
- A WebSocket must send a Protobuf `JOIN` as its first binary message. The server authenticates its token and verifies character ownership before inserting the player into the world.
- A later connection for the same character replaces the active transport. The old connection cannot delete the replacement when it eventually closes.
- An account gets one body in the world at a time. `Engine.Join` refuses a join with `ErrAccountInWorld` when the account already occupies the world through a *different* character, and the socket answers with an error and closes. See [one body per account](#one-body-per-account).

Production deployment must terminate TLS in front of the process, rate-limit authentication endpoints, back up the database, and set an explicit trusted-origin policy if the frontend and backend are split across hostnames. Email verification and password recovery are not present in this rudimentary account implementation.

### Administrator authorization

`data/tuning/admins.json` is the server-side list of administrator account emails. Both configured values and stored account emails are trimmed and compared case-insensitively, matching the registration/login identity rule. The table is embedded, validated for malformed and duplicate normalized values, and takes effect on rebuild/restart. It is deliberately not imported into the browser bundle.

Authentication produces an `auth.Principal` containing the persisted account ID, email, and server-derived admin bit. The API recomputes that principal on every authenticated request by joining the opaque session to its account, so an existing session follows configuration changes after a restart. `api.withAdmin` is an opt-in boundary for privileged HTTP features: it distinguishes a missing/expired session (`401`) from a signed-in non-admin (`403`). The `is_admin` value returned to the client supports presentation only and is never accepted back as authority.

Developer mode is the first protected feature. Spawnability and form schemas live with each archetype in `data/tuning/entities.json`: fields name a stable `component.attribute` binding, spawn/edit scope, and number/text/select/position/rotation input contract. The client builds the catalog and forms from those rows, while the server parses the same data and validates every value. `game.adminAttributeRegistry` is the sole explicit adapter from stable bindings to today's embedded structs; it avoids reflection and can be retargeted to ECS component stores without changing tuning, HTTP, or the UI. `/api/admin/spawn` places an archetype — including a standing smoke field, so occlusion can be looked through without a Gunslinger having to throw one — and `/api/admin/entity/inspect`, `/edit`, and `/delete` target any live entity selected from a snapshot. The legacy caller-only `/api/admin/attributes` route remains compatible through the same registry. Material-grant bounds live at `materials.admin_grant` and level-grant bounds at `progression.admin_grant`. Every command verifies administrator status and controlling-character ownership before crossing the engine seam; the world never receives an account token. Operational details are in [`administration.md`](administration.md).

`transform.position` is one canonical `[x,y]` value in the registry and API. The client renders it as adjacent unbounded number boxes plus a one-click world-position picker, while the server validates both coordinates against tuning and the entity's world extent. `transform.heading_degrees` is a rotation input and adapter: it rotates projectile velocity without changing speed and updates telegraph direction. Generic numeric HTML inputs omit `min`, `max`, and `step`; those constraints remain in tuning and are enforced by `validateAdminValue`.

## Persistence and migration

Two independent versions govern a save, and neither one is the tuning tables'.

`PRAGMA user_version` records how far the *database schema* has been migrated. `store.migrations` is an append-only list whose index+1 is the version it leaves behind; opening a database applies every migration it has not seen, each inside a transaction with its own version bump, so an interrupted upgrade is never recorded as complete. A database written by a newer build is refused rather than downgraded. A v0.1 database predates the version counter, reports 0, and migrates forward in place because migration 1 is written idempotently.

`characters.schema_version` records the *record* shape, which changes independently of the table layout. `model.Character.Migrate` carries an older row forward through sequential steps inside the store's scan path, so nothing above the store ever sees an older shape, and `SaveCharacterState` stamps the current version when the record is next written. Version 1 is the original name/class/level/xp record; version 2 adds saved world position, carried materials, and unlocked outposts; version 3 adds the last-seen stamp that decides whether that position is still honoured; version 4 adds the equipped loadout; version 5 adds the permanent unlock ledger. Neither counter is the tuning tables' `schema_version`, which reached 7 for the first developer catalog, 8 for slot/gadget data, 9 for progression/unlock levels, 10 for common entities and fixtures, 11 when admin schemas moved onto entity archetypes, 12 for vector-position and rotation admin inputs, 13 for gunplay and special ammunition, and 14 for deployable fields and the developer level grant. A table-shape change needs no character migration because these instances are derived/in-memory rather than persisted. A record from a newer build is an error, because writing it back would truncate fields this build cannot see.

A character keeps its world position, carried raw materials, unlocked outposts, and equipped loadout across a disconnect: position in nullable scalar columns (unplaced and placed-at-the-origin are different states), materials, outposts, and the loadout as JSON on the same row, since the access pattern is always whole-character. Crafted items live in `crafted_items` as a blueprint ID plus a slot → component ID map — references only, never a stat snapshot, so a balance edit retunes every owned item in place. Crafting itself arrives in Phase 2.3; the record contract does not wait for it.

### Logout, linger, and recall

Dropping the connection does not remove the body. `Engine.Leave` unregisters the client and starts a logout window — `session.logout_linger_seconds`, 10 as shipped — during which the body stands in the world: it takes damage and can be killed, but it cannot move, dash, or fire, because `stepPlayer` treats a lingering player the way it treats a dead one. Snapshots flag it as `lingering`, and the client lowers its opacity and marks the plate “offline,” so attackers do not mistake it for an active player. Disconnecting is therefore not an escape from a fight. Reconnecting inside the window resumes that same body wherever the fight has since moved it, which doubles as the reconnect path. Resuming clears the body's input state: the input sequence numbers the connection, not the character, so a fresh client counting from one again would otherwise have every input rejected as stale by `ApplyInput` and every predicted input discarded against a stale acknowledgement. Only a living body is resumed — a body killed inside its window is dropped on reconnect and the character enters at the hub, so being killed while logged out costs the position exactly as it would had the window closed first and the unplaced save decided the entry. When the window closes, the tick loop reaps the body, saves its final position, and removes it.

### One body per account

An account may have only one character in the world. `World` indexes bodies by account — `occupants`, maintained by `AddPlayer` and `RemovePlayer` — and `Engine.Join` refuses with `ErrAccountInWorld` when that index already holds a character other than the one joining. Rejoining as the *same* character is untouched: that is the reconnect and replacement path above.

The occupancy follows the body, not the connection, so it outlasts a disconnect for the length of the logout window. This is deliberate. The alternative — evicting the other character on demand — would hand every player a combat-log escape: drop the connection mid-fight, join a second character, and the body that was supposed to stand there for ten seconds disappears. Refusing the join instead makes the linger window the only way out, and the cost is bounded by that same window: after a normal exit, the second character can enter as soon as the first body is reaped. The refusal reaches the player as the transport's `SERVER_ERROR`, which the client shows on its terminal connection overlay with the return-home action.

Characters with no account ID are never indexed, so the simulation tests, which build characters without one, do not contend for a single shared slot.

A saved position is honoured only while it is fresh. Every save is stamped, and on join a stamp older than `session.position_expiry_seconds` — 30 minutes as shipped — stops being trusted: the character is recalled to the safe fixture nearest to where it logged out, choosing between its unlocked outposts and the central hub. `data/tuning/outposts.json` holds those positions and ships empty, so today every recall resolves to the hub; Phase 3 adds rows, not structure. An unstamped position (a record migrated from schema version 2) counts as expired, because there is no way to tell how old it is. A position is also refused when the world can no longer accept it — outside the rim, or inside cover that did not exist when it was written — and falls back to the deterministic hub spawn ring.

The engine owns save timing through the narrow `game.Persister` interface, so the simulation still has no database dependency. Writes leave the tick loop through a buffered channel into a single writer goroutine, so a slow database can never delay a tick; an overflowing queue writes on its own goroutine rather than dropping the save. Saves happen when a logout window closes, every 15 seconds for everyone present, and once more for everyone present at shutdown, lingering bodies included. A connection that has already been replaced touches nothing, or it would strand or overwrite the live session. A dead player is saved unplaced and re-enters at the hub, matching the current instant respawn until Phase 4.2 replaces both.

Because a save stores references, content can only be withdrawn, never deleted. `data/tuning/retired.json` maps every retired ID to a live replacement or a material refund, validated for dangling and circular chains at load, and `Tables.Resolve` follows it. Carried materials pass through it on join, so a retired material arrives as its replacement or its refund instead of vanishing. The client does not bundle this table: retirement resolves persisted references, and the client holds none.

## Simulation and world rules

`game.World` owns all mutable gameplay state and advances on a fixed 60 Hz tick. `game.Engine` serializes joins, leaves, inputs, respawns, and simulation steps with a mutex. The default world is a 3,000-unit-radius contiguous circle with a 430-unit central service-safe hub, a PvP-protected fringe out to 1,000 units, and deterministic procedural trees outside the hub.

Implemented authoritative rules include:

- normalized eight-direction movement at a capped speed;
- a timed dash that carries the player at high speed along the direction locked in at the press (movement input, or aim when standing still) for a whole number of ticks, overriding steering for its duration and colliding normally;
- circular player/world bounds plus circle and axis-aligned-box world-item collision, resolved independently on each movement axis;
- server-authoritative aim, universal dash, dash cooldown, health, Mage mana/regen, Gunslinger magazine/reload, ability cadence and cooldown, projectile lifetime, damage, death, and central-hub respawn;
- one ability path for every deliberate action, and one status-effect layer applied through it (see [abilities and effects](#abilities-and-effects));
- effective-damage attribution and most-contribution kill credit through a bounded combat log;
- the nine settled gun categories, separated by handling rather than by damage: magazine, reload, range, recoil pattern, spread, weight class, scope, pellets, and blast (see [gunplay](#gunplay));
- approximately three-second uncontested raw TTK from the shared 100-health/10-damage/300-ms-cadence tuning band;
- no PvP damage inside the protected hub/fringe, while projectiles may still resolve visibly;
- deterministic ordering for player and projectile processing and deterministic tree generation.

### Common entity and collision model

Every materialized simulation object embeds `game.Entity`: players, projectiles, telegraphs, procedural trees, and authored walls therefore share ID/kind, position/velocity, mass, current/max health, alive state, and a list of collision objects. This is a typed data base rather than an interface hierarchy. `Player`, `Projectile`, and `Telegraph` keep only their family-specific state beside it, while fixed and procedural terrain live directly as entities in a dense `World.worldItems` slice. Hot movement and projectile loops iterate that slice without interface dispatch; the same layout can later be split into ECS component columns without changing gameplay semantics.

`data/tuning/entities.json` owns the defaults. A runtime entity receives a copy of its archetype, so mutable health or geometry never changes the shared row. `EntityOverrides` uses pointer-valued scalar fields and an explicit collision-list replacement: zero is a valid override, and an empty collision list is distinct from no override. The shipped tree archetype has mass `-1`, 500 health, and one circular collision object; the procedural generator overrides that circle's radius per instance. The shipped wall has mass and health `-1` and one 96 × 96 box, and `world.fixtures` places `wall-00` at `(650, 0)`. Negative mass means immovable; negative health means undestroyable. Projectiles damage destroyable terrain through the same `Entity.TakeDamage` primitive used beneath player damage, and a destroyed tree immediately leaves both authoritative collision and AOI snapshots.

Collision objects are local primitives with an optional offset. Circle and axis-aligned box are implemented. The server sends AOI-filtered collision components separately from visual entity state so client prediction can keep a compact hot set. Each snapshot replaces that set rather than merging it, which removes a destroyed tree or an exited-AOI fixture immediately. Multiple objects per entity are supported by unique component IDs even though the shipped archetypes currently use at most one.

Every family promotes `Entity.Delete(time.Time)` and satisfies the small `Deletable` lifecycle interface. Forced deletion immediately leaves action and collision, remains in snapshots for a 350 ms normalized fade plus a short snapshot-delivery grace, and is then collected by its owning store. Connected players deliberately remain as ordinary dead bodies after the fade so the existing death overlay and respawn path stay valid; respawn clears deletion state. Disposable admin players and non-player entities are physically reaped. The wire carries `deleting` and `delete_progress`, allowing the frontend to fade deliberately instead of guessing whether snapshot absence means removal or AOI exit.

### Abilities and effects

Every deliberate action runs through one path. `data/tuning/abilities.json` owns the contract — cost, cadence, cooldown, windup, telegraph, dodge vector, delivery, and the status effects a hit applies — and weapons and spells hold only identity and point at it: a gun names the ability it fires, a staff names a spell that names the ability it casts. `game/ability.go` is the executor. `World.ability` resolves what the use button performs, `World.useAbility` gates it on the shared cadence and the ability's own cooldown, `World.spend` charges the declared cost, and `World.deliver` either launches immediately or creates a committed windup. Nothing branches on class or weapon shape: a magazine and a mana pool are two cost kinds on one path, and a spent magazine commits to its reload from inside `spend`. `World.ability` resolves the [selected loadout slot](#loadout-and-slots) rather than the weapon directly, so a gun, a gadget, and a spell all arrive as one shape; Phase 4.3's mobs call the same owner-agnostic telegraph emitter and delivery path. Each ability keeps its own lockout in `Player.Cooldowns` on top of the global `NextFire` cadence, which is the second resource axis the Mage kit is built on.

`game/effects.go` is the status layer: burn, slow, root, stun, knockback, shield, and blind. `Player.Effects` holds running statuses by effect ID rather than by value, so a balance edit retunes a status already on a body. `World.stepEffects` runs before the body acts and ticks burns in whole cadences, `World.damage` is the single path health is lost through — shields absorb first, and death clears everything the body carried — and `World.applyEffects` starts statuses from the projectile that carried them. Movement reads the layer rather than the reverse: a stun suppresses movement, dash, reload, and use; a root takes only movement; slows scale speed and take the strongest rather than compounding into an unanswerable root; a knockback overrides input and cancels an in-flight dash. PvP protection covers the status exactly as it covers the damage, because a slow landed from inside safety would be the offensive use of a safe zone the [invariants](game/design/invariants.md) forbid.

`data/tuning/effects.json` carries two shipped rows from Phase 2.4: the knockback a rocket's blast applies, and the blindness a flashbang leaves — the one status that changes what a player is *sent* rather than what their body does. The other five kinds still run against rows the tests author themselves, because no design document has settled a magnitude for them yet; Phase 2.5's element secondaries fill the rest. Active effect IDs travel on player entities, so those content rows need no later protocol change.

Windups live in `game/telegraph.go`. A use pays its cost and starts its cooldown immediately, locks authoritative origin and aim, and emits one AOI-culled `TELEGRAPH` entity. Pending ends at `windup_ms`; delivery happens once on that transition, active persists for the row's `active_ms`, and resolved supplies the flash/fade for `resolved_ms`. Death cancels a pending delivery directly into resolved. The fixed geometry is important counterplay: a caster may move or turn after committing, but cannot drag a warning over a player. Immediate abilities retain rewind; a windup starts at server receipt and is not backdated, so a client cannot erase the visible warning by claiming an old fire time. Circle, cone, line, and ring are validated shapes with one wire and renderer path. The starter Fire bolt exercises the line path; the Sentry row selects that same shape vocabulary while its timing, projectile, and AI remain Phase 4.3 content.

### Gunplay

`game/gunplay.go` owns where a shot actually goes and what a committed stance costs. Nothing in it reads a class: a weapon either declares a recoil pattern, a spread, a scope, or a guard, or it does not, so a staff runs the same path and simply has none of them.

**Recoil is a fixed pattern, never a random cone.** Each gun declares `recoil.pattern` — signed degrees, one entry per successive shot — and each entry is a *step* applied to a muzzle offset that persists between shots, so the weapon walks a shape rather than jumping between a few fixed angles. Every pattern's first entry is zero, so a settled first shot is always true. `recovery_ms` of quiet settles the offset linearly back to aim and returns the pattern to its first entry, which makes burst discipline the thing that controls a weapon; `Recoil.MaxDegrees` bounds the accumulation at half of one full walk of the pattern, because a magazine longer than the pattern repeats it. Because the pattern is data and deterministic, it is a shape a player learns and compensates for rather than a dice roll, and two characters with the same category fight with the same recoil.

The offset is a pure function of the last shot and the time since — `World.recoilDegrees` — rather than something integrated per tick, so a rewound resolve and a snapshot always agree on it. It travels on the wire as `Entity.recoil_degrees` alongside `Entity.shots`, the body's total shot count, because recoil is only a skill axis if it can be *seen*: the renderer rotates the drawn weapon by the offset, and every increment of the shot counter shoves the weapon back along its own axis with a muzzle flash — plus a camera knock for the local body. Gunplay belongs to the weapon slot alone, so a thrown gadget never walks the gun's pattern.

**Move-spread is the accuracy-against-mobility trade.** A weapon declares a standing floor and a moving maximum, and the simulation interpolates between them by how fast the body is actually travelling. The magnitude is drawn from `splitmix` seeded by the shooter's ID and its own total shot count, so a shot is exactly reproducible in a test while remaining unpredictable in a fight, and no shared RNG state couples two players.

**Weight class is the balance axis and never touches damage.** `combat.weight_classes` scales movement, recoil, and move-spread; `World.handlingScale` multiplies the weight class, an active scope, and a raised shield, and composes with `World.movementScale`, which is what statuses do — a slowed body carrying an LMG pays both. The client applies the identical multiplier through `Predictor.step`, so a scoped or shielded step does not rubber-band.

Delivery gained three shapes, all validated at load. A **multi-pellet** projectile fans deterministically over its declared cone and divides the band's damage between its pellets, so a full connect is worth exactly one band hit and the shotgun's identity is the condition it imposes rather than extra damage. **Falloff and hard maximum range** live on the projectile: a round tracks how far it has flown, decays linearly from `falloff_start` toward `falloff_min`, and stops at `max_range` regardless of lifetime left. A **blast** resolves at the impact point, reaching everyone inside its radius with the band and the effects it declares.

**Hitscan is the sniper's exception and it is gated.** An ability may only land instantly if it declares `requires_scope`, and it must claim the `scoped_commit` dodge vector, which names what replaces travel time: the peripheral blackout and movement penalty of scoping. `World.hitscan` resolves along the line with the same rewind a fired projectile gets, cover stops it before anything behind it, and if nothing is reached inside the cap the round continues as an ordinary travelling projectile that starts already carrying the distance it covered instantly. Scoping is held rather than toggled, widens the snapshot the server sends by `scope.view_bonus`, and reaches the wire as `Entity.scoped`, because the commitment is what an opponent plays around.

**The riot shield** is a gadget whose ability declares a `guard` instead of a delivery: while its slot is selected, holding the use button raises it. It blocks bullets and projectiles arriving inside its frontal arc, measured against where the body is aiming; it slows its user; and it locks fire, since the button that holds it up cannot also shoot through it. It does not stop a blast that goes off around it, which keeps it an answer to a burst opener rather than an answer to everything.

**Smoke and flashbangs are deployables and control, never damage.** A gadget ability may declare a `deployable` — an entities.json archetype, a radius, a lifetime, and a reveal radius — and `game/deployable.go` materialises it where the thrown canister stopped. A field is not a collider: it changes what can be seen, never where a body may walk, and validation refuses an archetype with collision geometry. `World.occluded` is the whole vision rule: a cloud on the segment between two points hides one from the other, and two bodies inside the field's reveal radius always see each other, so standing in your own smoke is not suicide at contact range. This is deliberately *not* general line of sight — cover and walls remain Phase 2.6's substrate — it is only the case a canister is bought for. A flashbang needs no field at all: it is an ordinary blast that applies a `blind` status, and `World.blinded` is checked once at the top of `SnapshotFor`.

Both are enforced on the wire rather than drawn over on the client. A blinded viewer is sent nothing but its own body; an occluded one is not sent what the cloud covers. Terrain is the deliberate exception in both cases: static cover blinking in and out as a cloud drifts would desynchronise the client's own collision prediction, and the cloud drawn over it already hides it. The renderer draws a field as drifting puffs seeded from its own ID — nothing describing how a cloud looks travels on the wire — and paints a blindness as a white sheet over the stage for as long as the status runs.

**Special ammunition** is the exception to the magazine. A launcher's ability spends a carried material (`cost.kind: "material"`) rather than rounds, so it has neither a magazine nor a reload: when the stack is empty, it stops firing until more is built. `data/tuning/ammunition.json` holds the recipes, `World.CraftAmmunition` runs them through the same safe-zone gate an ordinary craft uses, and what they produce lands in the carried inventory — which means Phase 4.2's death drops will treat rockets as the carried, losable resource they are.

### Loadout and slots

`server/internal/loadout` owns what a legal equipped set is; it holds no world state, so the simulation, the store, and the client agree on one answer without any of them owning the rule. A set is content IDs by slot — one weapon, five gadget slots, six spell slots, from `data/tuning/loadout.json` — and never a copy of what those rows hold, so a balance edit retunes an equipped kit in place.

Both classes lay out over **one six-slot action bar**, which the tables enforce (`weapon_slots + gadget_slots == spell_slots`). A Gunslinger's slot one is its weapon and slots two through six are gadgets; a Mage's six slots are spells, cast through whichever staff it holds. `loadout.Equippable` intersects the live rows of the character's class with its unlock ledger, and `loadout.Validate` refuses unowned content on the mutation path, so hiding an option in the menu is never what stops it being equipped. `loadout.Bar` is the single answer to "what does the use button do", and `Input.selected_slot` travels on every input so the server resolves the button against the slot the player actually had selected. Keys 1–6 and the mouse wheel select it; touch gets six buttons. An empty slot performs nothing, which is a slot the player has not filled rather than an error; a Gunslinger's gadget slots hold the riot shield and whatever else `gadgets.json` has come to carry, and the smoke and flashbang deployables wait on Phase 2.6's line of sight. A Mage's slot one falls back to its staff's own declared spell, so a set emptied by a content withdrawal can still fight.

The Mage's affinity rule is the specialisation cost: a tier-N spell needs N−1 other spells of its element in the same set. The rule's shape is locked by [`mage.md`](game/design/mage.md#element-affinity) and only the multiplier is tunable, so the table validates that a tier-4 signature stays equippable inside the bar it shares.

**The safe-zone lock is the economy's keystone.** `World.SetLoadout` refuses any change from outside `world.SafeRadius`, and from a dead or lingering body, so owning more options improves preparation and never the power carried into one fight. It is enforced on the mutation, not on the UI: a client that ignores its own disabled controls is still refused. Respec inside safety is free — nothing is charged or consumed — and a committed change arrives as a fresh kit, full magazine and no cooldowns, both of which are only reachable where the lock already allows the change.

Commits travel over the same socket as everything else authoritative: `CLIENT_LOADOUT` carries the requested set, and the server always answers with `SERVER_LOADOUT` carrying the set that actually holds, plus a rejection reason when it refused. That reply is not terminal the way `SERVER_ERROR` is, so a refusal never drops the connection, and nothing is shown as equipped until it arrives. A committed set is persisted immediately rather than at the next autosave, because it is a deliberate commit rather than incidental world state. Loadouts cannot be edited outside the world at all: there is no HTTP mutation path, so a character logged out in the Deadlands cannot respec from the home screen.

The weapon slot holds something usable, and it may name either a `weapons.json` row — the stock configuration the starter kit and every level grant hand out — or a [crafted instance](#slotted-blueprint-crafting) of one. `crafting.Inventory.Equipped` resolves both to the same shape, so nothing downstream branches on which it was, and both are gated the same way: the instance must be one this character owns *and* its category must be in the ledger. Materials and components never reach the action bar; they live in the inventory surface.

`loadout.Resolve` runs on every join. Retired IDs follow the retirement chain, a withdrawn weapon is replaced by the class starter rather than leaving a character unarmed, and an arrangement a content change made illegal is repaired highest-slot-first so the casualty is the last thing equipped rather than the character's signature. An empty record resolves to the class default — the first weapon the character owns, plus the owned content of its slot kind packed from slot zero. Anything the [unlock ledger](#progression-and-unlocks) does not own is unequipped by the same pass, so a set can never outlive the ownership behind it. When the manifest `version` moved or the set had to change, the resolve reports the **global respec** the balance patch entitled the character to; since respec is already free, that grant is the re-validation plus the notice, and it clears the moment the player next commits.

### Progression and unlocks

`server/internal/progression` owns the permanent character axis: a **flat unlock ledger**, the XP curve, and the starter kit. Like `loadout`, it holds no world state, so character creation, the simulation, and the store reach one answer. A character record stores level, banked XP, and a sorted array of owned content IDs — never a derived stat and never a per-tree structure, matching the [flat-ledger contract](game/design/gunslinger.md#progression). Entries are only ever added: retiring content maps an entry onto its replacement, and a refunded retirement is owed against the material ledger rather than confiscating the slot.

Which content a level grants is declared on the content row itself. Every weapon, spell, and gadget carries an `unlock_level`, so adding a row means editing one table rather than keeping a second mapping in step, and `Tables.UnlocksThrough(level)` is a scan of those rows. Because the ledger stores bare IDs, validation requires unlock IDs to be unique across the three content tables and every row to unlock at a level inside the cap — content no level grants would be unreachable for anyone whose starter draw missed it.

**The starter kit** is rolled at character creation, not at first join, so a zero-material character owns a coherent loadout before it enters the world: one weapon drawn at random from its class's basic set, plus a random draw of the basic content of its slot kind, sized to fill the action bar. The draw is seeded from the character's own ID, so it is stable across re-derivation while two characters get different opening tools. Nothing drawn is exclusive — every row in the basic set also carries an unlock level, so a bad draw is a starting flavour, not a permanent gap.

`progression.Sync` runs on every join, before the loadout resolves, because what a character owns decides what it may equip. It follows retirements, tops the ledger up with everything the character's level has come to grant — which is how content added at a level a veteran is already past still reaches them — and rolls a kit for a record that predates the ledger entirely.

XP is awarded through named sources (`progression.sources`), a vocabulary the loader requires to be priced in full and refuses to extend from data alone. Only `player_kill` has a trigger today: `World.creditKill` awards it to whoever the [combat log credits](#damage-attribution-and-combat-log), so progression follows damage dealt rather than the last hit, and developer fixtures award nothing on either side. Mob kills, harvesting, and discovery are wired by the phases that build them. `progression.Advance` carries surplus XP into the next level and stops banking at the cap.

Because a player kill is the only trigger until Phase 4.3, content above the opening kit would otherwise be unreachable — and therefore unexercisable — on a fresh server. `POST /api/admin/progress` is the developer-mode answer, bounded by `progression.admin_grant` and running through the same grant path: it sets the level and adds everything the levels now held unlock. Lowering a level never confiscates an unlock, because the ledger is permanent by design.

A grant marks the body, and the tick loop drains those marks: each is persisted through `SaveCharacterProgress` — a separate statement from the world-state save, so a save written by a body that entered before a grant cannot roll it back — and pushed to its owner as `SERVER_PROGRESS`. The permanent axis also rides the welcome. Nothing about it is polled, so a level-up widens the Loadout section's options immediately.

### Slotted-blueprint crafting

`server/internal/crafting` owns what a build costs, what makes one legal, and what a finished item changes. Like `loadout` it holds no world state — the safe-zone gate needs a position, so `World.Craft` owns that — and it stores nothing derived: a crafted item is a weapon-row reference plus the component filling each blueprint slot, and every value it implies is recomputed from the tables at use time. A balance edit therefore retunes every existing crafted weapon in place, with no character migration.

Guns and staffs share one system. The weapon category is the blueprint; `components.json` declares the slots each blueprint exposes (muzzle, barrel, scope, trigger, magazine for guns; core, focus, conduit for staffs) and the components that fill them. An unfilled slot is the stock part, which is a legal choice — a build with no components at all is just the category.

**Components change behaviour, never the damage band.** Each carries an open `modifiers` map of multipliers over the numeric attributes the simulation reads: magazine size, reload, cooldown, windup, resource cost, projectile speed, lifetime, and radius, and — since Phase 2.4 delivers them — recoil degrees, standing and moving spread, and scoped movement. Damage is unreachable by construction, because it lives on the shared band row rather than on any numeric item field. The loader closes the remaining gap: it rejects a modifier on `interval_ms`, since fire cadence *is* the DPS axis and scaling it would move an item out of its band; it rejects any attribute the simulation does not read, so a modifier can never be data the world silently ignores; it bounds every multiplier to [0.5, 2] and rejects an exact 1 as a row claiming an effect it does not have; and it requires both a material cost and a plain-language `effect` string, because a component with no cost sits outside the economy and one with no description leaves a player spending materials on a mystery.

Modifiers merge multiplicatively across slots, so a build stacking two reload penalties pays both. A scoped movement modifier is clamped below one, so no component can hand back the mobility a scope is balanced on. `crafting.Apply` returns the modified weapon and ability together — they constrain each other, and a magazine and the round it spends have to stay coherent — and always works on copies, because the projectile row it scales is a pointer shared by every character firing that ability. A staff's components reach the spell it casts, since the staff is the delivery device; they never reach a gadget, which the weapon has no part in throwing.

**Crafting is gated to safe zones**, the other half of the [loadout lock](#loadout-and-slots): raw materials have to be hauled back before they become anything. `World.Craft` refuses from outside `world.SafeRadius` and from a dead or lingering body, then validates the recipe and the carried materials before it spends anything. A refusal is atomic — no item, no spend — and reports what is missing per material, because "you are short three tempered plate" is actionable and "you cannot afford this" is not. `CLIENT_CRAFT` carries the request and the server always answers `SERVER_CRAFT` with the authoritative owned items and carried materials, plus a rejection reason when it refused; like `SERVER_LOADOUT` that reply is not terminal, so a refusal never drops the connection and nothing is shown as spent before the server confirmed it. A successful craft is persisted immediately: the item insert runs before the state that paid for it, and a failed insert abandons that state too, so a craft never takes the materials without leaving the item behind.

A weapon row may also carry a material `cost` of its own, and the heavy categories carry `requires_craft`: they have no stock configuration at all and can only be carried as an instance that was actually built. That is how rare materials gate heavy weapons **economically rather than statistically** — every category still shares one damage band, and what rarity buys is commitment. `crafting.Inventory.Equipped` refuses a withheld row as a stock reference, so the gate holds on the mutation path rather than in the menu.

`progression.crafted_item_capacity` bounds how many crafted weapons a character may own: a stock build costs no materials, so without a ceiling a client could mint rows forever, and it is also the capacity outcome the crafting surface owes. Developer fixtures cannot craft at all — an admin-spawned body has no character row to hang an item off.

Nothing produces a material yet — harvesting is Phase 4.1 — so the only source today is a developer-mode grant behind the [administrator wrapper](#administrator-authorization), bounded by `materials.admin_grant` and validated against the live material rows.

### Damage attribution and combat log

`World.damage` records the health actually removed after shield absorption and caps it at remaining health, so neither shield pressure nor overkill inflates contribution. `combatLog` maintains a per-target ledger for the current life. A lethal hit freezes that complete ledger into a `CombatKill` event and chooses the largest contributor; an exact tie goes to the contributor whose first effective hit was recorded earlier. Last hit remains on the event as `SourceID`, but does not decide `CreditID`.

`World.Contributions` exposes a live ordered view for boss progress, `World.LastKill` exposes the immutable current-death decision for drop ownership, and `World.CombatEventsAfter(sequence)` is a bounded cursor stream for independent consumers. Respawn clears the mutable ledger and last-kill lookup, while immutable damage/kill events remain until the 1,024-event ring rolls over. Consumers must advance a cursor; the log is an integration surface, not durable storage.

Balance values live in the [tuning tables](#tuning-tables), never in the simulation source. `game.Tuning` is a derived runtime view of them: `game.FromTables` fills it, and only the process-level rates (tick, send, AOI, rewind) are overridden afterwards from configuration. Persistent records store references and progression fields, not computed combat values, matching the [progression persistence contract](game/design/progression-and-crafting.md#persistence-and-versioning).

## Tuning tables

`data/tuning/*.json` holds every balance number as versioned data. The Go server embeds the directory through `data/data.go` and parses it in `server/internal/tuning`; the Vite client imports the same files from `web/src/tuning.ts`. One directory, two consumers, no duplicated literal — a balance edit moves the authoritative simulation, client prediction, and the renderer together. `data/tuning/README.md` documents the file-by-file schema and the editing rules.

The tables cover simulation rates, session windows, common entity defaults and their admin metadata, world geometry and danger bands, fixed fixtures, the player body and universal dash, weight classes, damage bands, elements, abilities, status effects, weapons, spells, blueprint/component layouts and the components that fill them, material grades and rows, special-ammunition recipes, mobs, biomes, outposts, and retired IDs. Rows exist only where a design document has settled the content.

Damage is the clearest expression of the contract. No weapon, spell, or ability carries a damage number; a damaging ability points at a `combat.damage_bands` row, and weapons and spells carry no combat numbers at all — both delegate cost, cadence, cooldown, counterplay, and delivery to the ability they reach. Editing `damage_bands.standard.damage_per_hit` therefore retunes both classes at once, and a persisted character — which stores only IDs — needs no migration. `server/internal/game` has a test that replays stored character records against an edited copy of the tables to hold that invariant.

Loading validates rather than trusts. Unknown JSON fields are rejected, entity sentinels and circle/box geometry are checked, required archetypes must exist, fixed fixtures must reference them and remain inside the rim, administrator emails must be well-formed and unique after normalization, and entity admin fields must use `component.attribute` bindings with valid scopes and complete bounded inputs. Every cross-table reference is resolved, danger bands must run outward from the hub to the rim with contiguous PvP protection, projectile kinds must be unique across tables so the renderer can resolve a silhouette from a snapshot alone, and every damaging ability must declare both a shared damage band and a dodge vector the simulation actually delivers. Gunplay carries its own rules: every gun needs a recoil pattern that recovers and actually walks, moving spread must exceed standing spread, a weight class may only slow its carrier, an instantly landing round must require a scope and claim `scoped_commit`, a shield may not also deal damage, a withheld category must cost materials, and crafted ammunition must be produced by a recipe and never cost the material it makes. A projectile with zero travel speed is rejected as instant point-and-click damage; cast-time and ground-telegraph claims are rejected unless their windup/shape exists. Component modifiers are held to the behaviour axis: fire cadence is refused outright, an attribute the simulation does not read is refused, multipliers are bounded, and every component must cost live materials and describe itself in plain language. Telegraph geometry is shape-specific and phase durations must be positive. All problems are reported at once. `manifest.json` carries a content `version`, bumped on any balance edit and intended to drive the global respec/refund, and a `schema_version` that must match `tuning.SchemaVersion`; a mismatch fails the load with a request for the forward migration.

Because the tables are bundled into the client at build time, the server and the client must ship from the same build — which `make build` and the Dockerfile both do. Delivering tables to an already-running client, so that simulation constants can move without redeploying, is the separate versioned welcome/tuning message tracked in [`TODO.md`](../TODO.md) Phase 8.

## Network model

`proto/game.proto` is the protocol contract. The game socket accepts and emits only binary Protobuf frames; REST uses JSON because those low-frequency account surfaces benefit from ordinary HTTP semantics. The repository includes small schema-specific codecs in Go and TypeScript instead of requiring `protoc` at runtime. Golden-wire tests detect schema-number or wire-type drift.

Entity types reserve distinct paths for player, projectile, mob, drop, node, telegraph, deployable, boss, and world item. Common optional fields carry mass, health, element, squad ID, viewer-relative allegiance, invulnerability, logout linger, active effects, and telegraph phase/geometry. Each world-item collision component carries its owning entity, shape, and circle or box dimensions. Protobuf omits zero/default fields, keeping unused future state cheap. Input bit 128 is `INTERACT`; desktop binds E and touch exposes Use. Allegiance and telegraph phase are compact enums so adding content does not introduce ad-hoc strings into the 20 Hz path.

Defaults:

| Concern | Value | Behavior |
|---|---:|---|
| Simulation rate | 60 Hz | Fixed authoritative physics and client prediction |
| Snapshot send rate | 20 Hz | One AOI-filtered snapshot per client every third tick |
| Client input rate | 60 Hz | Sequence-numbered current input state |
| Interpolation delay | 100 ms | Remote entities render between buffered snapshots |
| Rewind window | 200 ms | Client fire time is clamped to this server window |
| AOI half-extent | 1,200 units | The full 2,400 × 2,400 camera square is retained; entities beyond either axis are culled |

### Snapshot bandwidth budget

`protocol.TestSnapshotBandwidthBudget` measures deterministic fixtures through the production encoder. For 20 players, 40 projectiles, and 30 world-item entities plus their collision components, the pre-expansion fields encode to 8,354 bytes and the expanded entity state to 9,720 bytes: +1,366 bytes, or 16.3%. Ten live line telegraphs bring that representative combat snapshot to 10,900 bytes (218,000 bytes/s at 20 Hz, before WebSocket/TCP/TLS overhead). The equipped loadout is deliberately not in that figure, and neither are owned crafted items or carried materials: all three travel on the welcome and on their own replies, never on a snapshot, because each changes only on a deliberate action inside a safe zone and paying for them twenty times a second would be pure waste.

The development dense fixture — 100 players, 200 projectiles, 25 telegraphs, and 80 world items with collision components — is 43,785 bytes, or 875,700 bytes/s at 20 Hz before transport overhead. The enforced guardrail is 64 KiB per AOI snapshot (1.25 MiB/s at 20 Hz). This is a ceiling, not a production target: Phase 8 still owes load tests, spatial indexing, delta/compressed snapshots, and priority tiers before the 100+ player claim is production-ready. Any schema or density change that crosses the guardrail fails the protocol suite and must bring a bandwidth strategy with it.

The rectangular camera AOI covers `4 / π` (about 27%) more area than the former inscribed circular cutoff at uniform density. That cost is deliberate so visible corners never lose state, but the byte guardrail and Phase 8 spatial-index/load-test work remain necessary before raising the configured half-extent.

### Prediction and reconciliation

The client applies local movement and dash immediately at the same fixed rate as the server and retains each input sequence plus its predicted motion. Because the dash is spread over a fixed tick count instead of a single displacement, the client counts down the same dash ticks at 60 Hz and records one motion per input, so replay reproduces the server path exactly. Every authoritative local-player snapshot carries `acknowledged_input`. The client resets to that position, removes acknowledged motions, and replays the remainder through the same circle/AABB collision rules. The current snapshot's AOI collision components replace the previous set before reconciliation, so destroyed or out-of-range world items stop blocking predicted movement.

The server never accepts client position, health, damage, cooldown, ammo, or mana values. It accepts buttons, aim direction, input sequence, and client fire time only. Invalid/stale sequences are dropped.

### Entity interpolation

Remote entities retain up to eight received samples. Rendering occurs 100 ms behind receipt time and linearly interpolates position and aim between the two surrounding samples. Telegraph progress interpolates only inside one phase; phase changes remain discrete so an active/resolved state is never pulled earlier by interpolation. Graceful-removal opacity follows authoritative deletion progress. The local actor uses its predicted position instead. Entities absent long enough to leave the AOI are destroyed client-side.

### Lag compensation and server rewind

The world retains a short timestamped position history for every player. When firing, the server:

1. clamps the claimed client time to `[server_now - max_rewind, server_now]`;
2. samples the shooter at that historical time;
3. creates the projectile at that historical muzzle position;
4. fast-forwards it in fixed substeps, testing against interpolated historical target positions and current static cover;
5. inserts a surviving projectile into the live simulation.

This preserves the [projectile dodge requirement](game/design/invariants.md) while compensating for reasonable network delay. It does not turn ordinary weapons into hitscan. Equal-timestamp history samples prefer the latest state, which is important for respawns and authoritative repositioning.

### Backpressure and connection lifecycle

Each client has a two-message outbound queue. If a slow client fills it, an older snapshot is discarded for the newer authoritative state rather than blocking the world tick. WebSocket reads are limited to 2 KiB, joins have a deadline, ping/pong detects dead peers, writes have deadlines, and reconnect attempts are exponentially backed off and capped at five. During reconnect the rendered world remains visible under an input-blocking overlay.

## Rendering and interface

Pixi.js is the committed 2D backend. Its WebGL batching of `Graphics` primitives covers the 100-player density target, and the design's [procedural visual language](game/design/visual-direction.md) needs no texture pipeline, so nothing in the effect ceiling argues for a heavier engine. The renderer stays behind `view.ts`, so this is a replaceable dependency rather than an architectural commitment.

Pixi owns only the play space; DOM elements own forms, dialogs, HUD, status, and accessibility semantics. The world renderer uses `Graphics` geometry for the grid, safety/world rings, actors, held weapons, projectiles, telegraphs, health bars, names, circular trees, and square walls. Damaged destructible terrain gets a compact health bar; undestroyable fixtures do not. Fill communicates class/element identity while outlines communicate self/squad/neutral/hostile allegiance, with silhouette providing the required redundant non-color channel. The shared telegraph layer renders table-driven circle, cone, line, and ring shapes beneath actors; opacity intensifies pending warnings, holds active areas, then flashes and fades resolved geometry. Because the server serves a strict `Content-Security-Policy` with `script-src 'self'` (no `unsafe-eval`), `view.ts` imports `pixi.js/unsafe-eval` for its side effect, which swaps Pixi's runtime code generation for eval-free polyfills before the renderer is created.

Desktop controls are WASD/arrows, pointer aim/fire, Shift or the secondary pointer button to scope, 1–6 or the mouse wheel to select an equipped slot, Space dash, R reload, and E interact. Scoping is the one camera exception the [HUD specification](game/ui/game-view-and-hud.md#camera) allows: a DOM vignette blacks out peripheral vision while the world camera is pushed halfway toward the aim, and `pointerWorld` compensates so aim stays relative to the body rather than to the shifted camera. A raised shield draws the arc it actually blocks, and a scoped body draws a sight line, so both committed stances are readable from outside. Touch-first layouts use fixed, independently captured movement and aim/fire sticks centered in their half-viewports, keep direct world-touch firing, place dash/interact and reload/scope above the corresponding stick, and place the six slot buttons in a safe-area-aware row above them. Source-keyed held inputs prevent one pointer release from cancelling another control. Hotbar DOM nodes survive high-frequency snapshot redraws, while hotbar/menu groups and important overlays activate directly on touch pointer-up. The fixed device-scale viewport blocks iOS gesture/double-tap/focus zoom; gameplay also disables text selection/callouts without disabling form-field editing. The HUD exposes health, class resource, the six-slot action bar with the selected slot marked by border and weight as well as fill, danger/PvP state, latency, reconnect state, death, and respawn. The compact top-right non-modal menu leaves movement/combat controls active and reacts to authoritative progression, inventory, safety, health/resource, zone, loadout, and selected-entity changes. Updates are coalesced to an animation frame. Fine-pointer clients collapse it shortly after pointer exit and expand it on hover; touch contact never runs those hover handlers and uses the explicit minimize control. Its Loadout section is viewable anywhere and editable only inside a safe zone, where it states the lock reason rather than silently disabling controls. Home authentication and character creation remain modal.

## Configuration

| Environment variable | Default | Meaning |
|---|---|---|
| `SPELLFIRE_ADDRESS` | `:8080` | HTTP/WebSocket listen address |
| `SPELLFIRE_DATABASE` | `spellfire.db` | SQLite path; use `:memory:` only for one-connection tests |
| `SPELLFIRE_WEB_ROOT` | `dist` | Built frontend directory |
| `SPELLFIRE_TICK_RATE` | `tick_rate` | Authoritative ticks per second |
| `SPELLFIRE_SEND_RATE` | `send_rate` | Snapshots per second |
| `SPELLFIRE_AOI_RADIUS` | `aoi_radius` | Per-player interest half-extent on each camera axis |
| `SPELLFIRE_MAX_REWIND_MS` | `max_rewind_ms` | Maximum accepted lag-compensation age |

The last four default to the matching field in `data/tuning/simulation.json` (60, 20, 1200, and 200 as shipped), so an unset environment reproduces exactly what the client bundled. Tick and send rates must remain evenly compatible for predictable snapshot pacing; the loader rejects a table whose tick rate is not a whole multiple of its send rate. Overriding either through the environment moves the server away from the client's compiled-in prediction constants, so production should expose those values through a versioned welcome/tuning message before making them live-configurable.

## Container deployment

`Dockerfile` is a three-stage build: a `node:20-alpine` stage runs `npm ci && npm run build` to produce the client bundle, a `golang:1.22-alpine` stage compiles the server with `CGO_ENABLED=0` (the SQLite driver is pure Go, so no C toolchain is needed), and an `alpine:3.20` runtime stage carries only the static binary and the built assets. The result runs as a non-root user (`spellfire`, uid 10001) and is ~21 MB.

Inside the image the binary is `/app/spellfire-server`, static assets live at `/app/web` (`SPELLFIRE_WEB_ROOT`), and the SQLite database is written to `/data/spellfire.db` (`SPELLFIRE_DATABASE`) on a mounted volume so account/character data survives container replacement. A `HEALTHCHECK` polls `/api/health`. The build stage stamps the build timestamp from the build host's clock and, if the `BUILD_COMMIT` arg is supplied, the git commit; pass it with `BUILD_COMMIT=$(git rev-parse --short HEAD) docker compose build` (the `.git` directory is not in the build context, so the commit cannot be read inside the image).

`compose.yaml` builds the image, persists `/data` in the `spellfire-data` named volume, and exposes the tuning environment variables (overridable through a sibling `.env` file). Run with `docker compose up --build -d`.

The service publishes no host port. It attaches to an external Docker network named `proxy` (create it once with `docker network create proxy`) and only `expose`s port 8080 on that network, so a reverse proxy sharing the `proxy` network reaches the server at `http://spellfire:8080`. The proxy must forward both plain HTTP (`/`, `/api/...`) and WebSocket upgrades (`/ws`) to that address. To run standalone without a proxy, add a `ports:` mapping back to the service.

## Testing and verification

Backend tests additionally cover typed entity overrides, tree destruction, undestroyable square-wall behavior, circle/AABB collision, crafting recipes, cost aggregation, per-material shortfalls, the safe-zone gate, atomic refusals, modifier application on both a gun's own ability and a staff's cast, the shared projectile row surviving unmutated, crafted items round-tripping through a rejoin, the bounded developer material grant, a canister deploying where it lands and expiring, smoke hiding what is behind it but not what is touching it, a flashbang blinding without damaging and emptying the blinded body's snapshot, recoil walking and settling back to aim, and a thrown gadget leaving the gun's pattern alone; plus effective-damage ledgers, most-contribution credit, deterministic ties, shield/overkill exclusion, combat-log cursors and life resets; windup cost/delivery, locked geometry, phase transitions and death cancellation; all four validated telegraph figures; every expanded entity/input wire field; and the 64 KiB dense-snapshot guardrail. Frontend tests cover circle/AABB prediction and collision removal, the crafting mirror — slot layout, fitting components, cost aggregation, shortfalls, plain-language behaviour, and a crafted instance resolving onto the action bar — plus expanded wire decoding, interact encoding, movement/dash prediction, shared-table derivation, and the pending/active/resolved opacity grammar including its resolution flash.

Run `make test` for Go tests, frontend tests, and strict TypeScript checking. Run `make build` to produce `dist/` and compile the Go server.

## Deliberate initial limitations

The foundation implements the requested accounts, multiplayer transport, authoritative combat/netcode, both player classes, procedural world, destructible collidable trees, one fixed undestroyable wall fixture, and slotted-blueprint crafting. The following GDD systems are represented in UI/reference language but are not falsely presented as functional: harvesting/material persistence and death drops, squads and squad loot, Sentry mobs, outposts/travel, marketplace, and world bosses. Their rules or values remain open in the source specifications, and each should be added as a separate authoritative module rather than embedded in `World`.

Other known limits are one world process, no spatial index beyond AOI distance checks, and no distributed session cache. The current O(players + projectiles + telegraphs + world items) per-client snapshot path is appropriate for development and modest concurrency. Rewind still tests the current world-item set rather than historical terrain health, so the narrow case where a tree is destroyed inside the 200 ms rewind window can disagree with the cover visible at the claimed shot time; dynamic player-authored walls remain blocked on implementing lifetime history in Phase 2.5. The byte budget is measured and guarded, but reaching the [100+ player design target](game/design/world.md) under battle density still requires load testing, a spatial hash/quadtree, snapshot deltas/compression, and priority tiers before production claims are made.
