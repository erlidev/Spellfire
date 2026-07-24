# SpellFire — Implementation Plan

Everything the [game design](docs/game/design/README.md) and the [user-facing specification](docs/game/ui/README.md)
specify that is **absent** or **partially present** in the code, ordered into dependency-driven phases.
Each task links to the document that owns its rule and, where useful, the code that must change.

Legend: `[x]` done · `[ ]` not started

Rules for this file: check a box only when the behaviour is implemented *and* covered by a test or an
explicit note in [`docs/architecture.md`](docs/architecture.md). Update the docs in the same change.

---

## Phase 0 — Shipped foundation (v0.1)

- [x] Accounts, opaque sessions, bcrypt hashing, character CRUD on SQLite
- [x] One shared 60 Hz authoritative world; engine serialises joins/inputs/steps
- [x] Eight-direction normalised movement, circular world bound, circle/box world-item collision
- [x] Universal dash — fixed distance, 2.2 s cooldown, no i-frames ([combat.md](docs/game/design/combat.md#universal-dash), [world.go:187](server/internal/game/world.go#L187))
- [x] One starter projectile attack per class; mana regen; magazine + reload
- [x] ~3 s raw TTK from a single 100 HP / 10 dmg / 300 ms tuning band ([combat.md](docs/game/design/combat.md#time-to-kill))
- [x] Hub + fringe PvP protection by radius from origin
- [x] AOI culling, server rewind, client prediction + reconciliation, entity interpolation
- [x] Procedural Pixi renderer: grid, bodies, held weapon, health bar, name plate
- [x] Home screen, auth modals, connection/reconnect overlay, HUD, in-game menu, touch controls
- [x] Docker image + compose deployment, build stamping, `make test` / `make build`
- [x] Renderer decision closed: Pixi.js behind `view.ts` ([architecture.md](docs/architecture.md#rendering-and-interface))
- [x] Mage ranged-poke gap closed as an intentional absence; five elements are final ([mage.md](docs/game/design/mage.md#elements))

---

## Phase 1 — Foundations

Nothing else in this file can be built cleanly without these. Land them first.

### 1.1 Versioned tuning tables
- [x] Move balance out of the `game.Tuning` struct literal into versioned data files ([data/tuning/](data/tuning/README.md); `game.FromTables` now derives `Tuning`)
- [x] Define the table schema for weapons, spells, components, materials, mobs, biomes ([progression-and-crafting.md](docs/game/design/progression-and-crafting.md#persistence-and-versioning), [tuning.go](server/internal/tuning/tuning.go), [validate.go](server/internal/tuning/validate.go))
- [x] Load the same tables in the client so the renderer derives from balance data, not TS literals ([tuning.ts](web/src/tuning.ts))
- [x] Verify the invariant in a test: editing one row changes every dependent item with no character migration ([game/tuning_test.go](server/internal/game/tuning_test.go), [tuning/tuning_test.go](server/internal/tuning/tuning_test.go))
- [x] Common typed entity base for every materialized world family, with tuning defaults and per-instance overrides; 500-health circular trees and an immovable/undestroyable square wall fixture ([entity.go](server/internal/game/entity.go), [entities.json](data/tuning/entities.json))

Biome rows carry their element, summary, and ambient palette since Phase 3.1, which also widened the material table to 43 rows across seven kinds; the remaining component and material rows landed with Phase 2.3. The Sentry row carries its settled contract without the values [economy-death-and-pve.md](docs/game/design/economy-death-and-pve.md#sentry) defers to implementation. Runtime table delivery to a live client stays in Phase 8.

### 1.2 Persistence & migration
- [x] Read `schema_version` and run sequential forward migrations — `PRAGMA user_version` for the database schema, `characters.schema_version` for the record shape ([sqlite.go](server/internal/store/sqlite.go), [model.go](server/internal/model/model.go), [architecture.md](docs/architecture.md#persistence-and-migration))
- [x] Retired-ID → replacement/refund resolution map; never delete an ID ([retired.json](data/tuning/retired.json), `Tables.Resolve` in [tuning.go](server/internal/tuning/tuning.go), [invariants.md](docs/game/design/invariants.md))
- [x] Persist character position, carried inventory, and unlocked outposts; saved every 15 s, at logout, and at shutdown, restored and re-validated on join ([engine.go](server/internal/game/engine.go), [world.go](server/internal/game/world.go))
- [x] Store crafted items as weapon + component IDs, never a stat snapshot (`crafted_items`; `model.CraftedItem`); Phase 2.3 is what writes them
- [x] 10 s logout linger: the body stays in the world, killable and unable to act, so disconnecting is not an escape ([session.json](data/tuning/session.json), [engine.go](server/internal/game/engine.go))
- [x] Saved position expires after 30 min offline and recalls to the nearest unlocked outpost or the hub ([outposts.json](data/tuning/outposts.json), `World.recallDestination`)
- [x] One body per account: a second character's join is refused while the first is in the world, lingering included, so switching characters is not a combat-log escape (`Engine.Join`/`ErrAccountInWorld`, [architecture.md](docs/architecture.md#one-body-per-account))

Unlocked outposts are mutated by the Phase 3.3 discovery trigger, which persists the unlock on the tick it happens rather than at the next autosave. Carried materials are now spent by Phase 2.3 crafting; harvesting, which is what legitimately produces them, is Phase 4.1. `outposts.json` carries real geography since 3.3, so a stale-position recall and a death both resolve to the nearest unlocked outpost. Lingering bodies are now flagged on the wire and rendered as dimmed, offline actors; Phase 7 still owns the surrounding exit and reconnect UX.

### 1.3 Server-side ability/effect framework
- [x] One authoritative ability system replacing the ad-hoc branches in `stepPlayer`/`tryFire` — `World.ability`/`useAbility`/`spend`/`deliver` ([ability.go](server/internal/game/ability.go), [architecture.md](docs/architecture.md#abilities-and-effects))
- [x] Ability schema declares cost, cooldown, telegraph, and **a mandatory dodge vector**; weapons and spells hold identity and reference it ([abilities.json](data/tuning/abilities.json), [tuning.go](server/internal/tuning/tuning.go), [invariants.md](docs/game/design/invariants.md))
- [x] Status-effect layer: burn/DoT, slow, root, stun, knockback, shields ([effects.go](server/internal/game/effects.go), [effects_test.go](server/internal/game/effects_test.go))
- [x] Reject at load any damaging ability with no declared dodge vector — and any that claims one the simulation does not deliver ([validate.go](server/internal/tuning/validate.go))

`effects.json` now carries a row of every kind the layer runs: Phase 2.4 shipped the rocket's knockback
and the flashbang's blindness, and Phase 2.5's element secondaries authored the rest, adding `armor` —
mitigation with no pool behind it — as the one new kind. Active effect IDs already reach the client in
the expanded entity state. Windups and telegraphs now run through the same ability path; the starter
Fire bolt exercises them without prematurely authoring Phase 2's element-secondary values.

### 1.4 Damage attribution
- [x] Per-target contribution ledger in `World.damage`; only effective health damage counts, after shields and capped before overkill ([combat_log.go](server/internal/game/combat_log.go), [combat_log_test.go](server/internal/game/combat_log_test.go))
- [x] Kill credit by most damage dealt, not last hit; equal totals resolve to the earliest contributor ([squads-and-world-bosses.md](docs/game/design/squads-and-world-bosses.md#squads))
- [x] Bounded cursor-based combat log plus immutable lethal event, reusable by drop ownership and boss ranking ([architecture.md](docs/architecture.md#damage-attribution-and-combat-log))

Contribution is scoped to one target life and resets on respawn. Damage and kill events remain in the
bounded stream after that reset, so Phase 4 drops and Phase 5 boss consumers can advance independent
cursors without retaining pointers into mutable `World` state.

### 1.5 Protocol expansion
- [x] Extend `Entity.Type` beyond `PLAYER`/`PROJECTILE` ([game.proto](proto/game.proto)): mob, drop, node, telegraph, deployable, boss, world item
- [x] Per-entity fields for element, allegiance/squad, telegraph state/geometry, invulnerability, logout linger, and active effect IDs
- [x] Add an interact button to the input bitfield and bind it to E / touch Use ([types.ts](web/src/types.ts), [main.ts](web/src/main.ts))
- [x] Measure snapshot size before and after; enforce and record the bandwidth budget in [`docs/architecture.md`](docs/architecture.md#snapshot-bandwidth-budget)

### 1.6 Telegraph grammar (built here, not in Phase 6)
- [x] Shared data-driven renderer for translucent circle / cone / line / ring ([visual-direction.md](docs/game/design/visual-direction.md#readability-system), [view.ts](web/src/game/view.ts))
- [x] Opacity encodes pending → active → resolved; resolution flash and fade are one shared style function ([telegraph.ts](web/src/game/telegraph.ts))
- [x] Spells emit the generic authoritative telegraph entity; the Sentry contract selects from the same validated shape vocabulary, and future mobs call the owner-agnostic emitter rather than a render-specific path

The starter Fire bolt now commits its cost and a fixed line telegraph for 300 ms before delivery.
Origin and direction lock at commit so a warning cannot track a dodging target; death cancels a pending
cast into its resolution flash. `active_ms`, `resolved_ms`, and shape geometry are versioned tuning,
while Sentry cadence, projectile values, and full AI remain deliberately deferred to Phase 4.3.

### 1.7 Administrator authorization foundation
- [x] Configure administrator accounts by normalized email in [`admins.json`](data/tuning/admins.json), with load-time validation and an empty secure default
- [x] Derive admin status from the persisted account on every authenticated HTTP request; expose the informational role through auth responses and `GET /api/account`
- [x] Provide and test an opt-in server-side admin wrapper (`401` unauthenticated, `403` non-admin) for future privileged features ([administration.md](docs/administration.md))
- [x] Entity-owned admin metadata with spawnability, generic input schemas, and explicit ECS-ready `component.attribute` adapters; pointer spawn/select/edit/delete for every live entity family, death-compatible player removal, and graceful frontend fades ([entities.json](data/tuning/entities.json), [admin.go](server/internal/game/admin.go))
- [x] Non-modal floating Field menu keeps movement/combat control while open and supports live admin pointer tools ([game-menu.md](docs/game/ui/game-menu.md), [main.ts](web/src/main.ts))
- [x] Reactive compact top-right Field menu with fine-pointer auto-minimize, manual touch minimize, vector world-position picking, and rotation controls; server tuning still validates numeric bounds ([main.ts](web/src/main.ts), [entities.json](data/tuning/entities.json))
- [x] Conditional in-game Admin tab with searchable catalog, persistent placement HUD, and bounded self speed/view-distance overrides

Developer fixtures and overrides are intentionally in-memory and reset with the
body/world. Each future administrative feature must register its server handler
through the authorization wrapper; client visibility alone never grants access.

---

## Phase 2 — Content axis

### 2.1 Equipped slots & loadout
- [x] Slot model: one weapon, five gadget slots, six spell slots, laid out over one six-slot action bar ([loadout.json](data/tuning/loadout.json), [loadout.go](server/internal/loadout/loadout.go), [progression-and-crafting.md](docs/game/design/progression-and-crafting.md#slots))
- [x] Server-side loadout validator: slot limits, class ownership, no duplicates, Mage affinity (tier-N needs N−1 same-element spells) — `loadout.Validate`
- [x] Loadout lock outside safe zones, enforced server-side on mutation — the keystone economy rule (`World.SetLoadout`/`ErrLoadoutLocked`, [architecture.md](docs/architecture.md#loadout-and-slots))
- [x] Free respec inside safety; global respec/refund on a balance patch — `loadout.Resolve` re-validates against the manifest version and reports the grant
- [x] Menu Loadout section: view anywhere, edit only in safety, with an explicit lock reason ([system-interfaces.md](docs/game/ui/system-interfaces.md#safe-zone-loadout-and-crafting), [main.ts](web/src/main.ts))
- [x] Slot selection: 1–6 and the mouse wheel on desktop, six buttons on touch, carried per input so the server resolves the use button against the selected slot ([game-view-and-hud.md](docs/game/ui/game-view-and-hud.md#slot-selection))

`gadgets.json` carries the riot shield, the smoke canister, and the flashbang from Phase 2.4; the
remaining slots stay empty, and an empty slot performs nothing rather than erroring. A Mage's slot one falls back to
its staff's declared spell, so a set emptied by a content withdrawal can still fight. Loadouts cannot
be edited outside the world at all — there is no HTTP mutation path — so logging out in the Deadlands
is not a way to respec. The equippable set is narrowed from "every
live row" to "what this character owns" by the Phase 2.2 unlock ledger.

### 2.2 Unlock ledger & starter kit
- [x] Flat permanent unlock ledger for gun parts and spells — sorted content IDs on the character record, never shortened ([model.go](server/internal/model/model.go), [progression.go](server/internal/progression/progression.go), [architecture.md](docs/architecture.md#progression-and-unlocks))
- [x] XP sources and a level → unlock mapping: [progression.json](data/tuning/progression.json) prices the sources and holds the curve, while each weapon/spell/gadget row declares its own `unlock_level`, so `Tables.UnlocksThrough` derives the mapping with no second table to keep in step
- [x] Starter kit on creation: one random basic class weapon + a random draw of low-tier unlocks, seeded from the character ID ([progression-and-crafting.md](docs/game/design/progression-and-crafting.md#starter-kit), `progression.StarterKit`, [api.go](server/internal/api/api.go))
- [x] Level now drives something: `World.creditKill` awards `player_kill` XP to the [damage-credited](docs/architecture.md#damage-attribution-and-combat-log) killer, `progression.Advance` grants what the levels crossed unlock, and the engine persists and pushes the change as `SERVER_PROGRESS`
- [x] `loadout.Equippable`/`Validate`/`Resolve` intersect with the ledger, so unowned content is refused on the mutation path rather than merely hidden by the menu
- [x] Test: a zero-material character can fill a coherent loadout immediately ([progression_test.go](server/internal/progression/progression_test.go)), plus starter-kit stability/randomness, level grants, ledger-gated validation, and the separate progression save path

`player_kill` and `discovery` have triggers — the latter since Phase 3.3 — while mob kills and harvesting are priced in the table and awarded by Phases 4.3 and 4.1, and Phase 4.4 tunes the curve against the pacing targets. Phase 2.4 widened the Gunslinger's basic set to four categories and its gadget pool to the riot shield, smoke, and flashbangs without touching the draw; a developer-mode level grant now reaches the rest of the ledger before mob XP lands; Phase 2.5 widened the Mage's draw to the five tier-1 spells, one per element, which is a legal six-slot set on its own because tier 1 needs no same-element company.

### 2.3 Recipe-blueprint crafting
- [x] Generic blueprints + independently costed component recipes + authoritative finished-weapon recipes; a complete arrangement resolves the result and ambiguous recipes fail tuning validation ([components.json](data/tuning/components.json), [crafting.go](server/internal/crafting/crafting.go), [progression-and-crafting.md](docs/game/design/progression-and-crafting.md#recipe-blueprint-crafting))
- [x] Realistic gun slots: receiver, barrel, action, feed, sight; explicit recipes for pistol, revolver, SMG, shotgun, service rifle, marksman, sniper, LMG, and launcher
- [x] Staffs are exactly one crafted mana crystal plus one wood-based stave; crystals apply bounded all-spell effects, staves apply no effects, and stave tier must meet or exceed crystal tier
- [x] Ash / runed oak / resonant ironwood stave tiers, with metal bands and magical infusions added to the higher-tier material recipes
- [x] Crafting gated to safe zones; raw materials must be hauled there (`World.Craft`/`ErrCraftingLocked`, [architecture.md](docs/architecture.md#recipe-blueprint-crafting))
- [x] Crafting UI: generic blueprint silhouette, drag/drop blanks with click/tap fallback, result preview, recipe list with explanations, staff subassembly/tier presentation, material shortfalls, spend confirmation, and rejection outcomes ([system-interfaces.md](docs/game/ui/system-interfaces.md#safe-zone-loadout-and-crafting), [crafting.ts](web/src/game/crafting.ts), [main.ts](web/src/main.ts))
- [x] Crafted items are equippable: the weapon slot names either a stock row or an instance, resolved through `crafting.Inventory.Equipped` on the same path
- [x] Test: all ten finished recipes resolve from parts, incomplete/incoherent recipes fail, staff tiers are enforced, all-spell damage/healing/cooldown modifiers apply, costs aggregate, refusals are atomic, shared rows survive unmutated, items round-trip through a rejoin, and inventory capacity holds

Components declare an open `modifiers` map, but only over attributes the simulation consumes, and the loader
rejects `interval_ms` outright. Gun parts remain primarily handling/reach choices; mana crystals are the bounded
exception that may alter all-spell damage/healing without editing a spell-specific row. Staves deliberately have no
modifier: their wood and infusions establish containment tier. Crystal and stave recipes commit atomically into the
finished staff rather than creating loose intermediate inventory. Pre-revamp component IDs retire onto the new catalog
and are moved to their live slot when an old crafted item rejoins.
Nothing produces a material yet — harvesting is Phase 4.1 — so the only source is the bounded
developer-mode grant behind the Phase 1.7 authorization wrapper, and an ordinary player sees accurate
shortfalls until then. Death drops (Phase 4.2) will drop carried materials and keep crafted gear, which is
already the split the inventory surface states.

### 2.4 Gunslinger kit
- [x] Recoil model: a fixed left/right pattern unique to each gun, *stepped* per shot from where the last one left the muzzle and settled back to aim after a quiet window, scaled by weight class ([gunplay.go](server/internal/game/gunplay.go), [weapons.json](data/tuning/weapons.json), [architecture.md](docs/architecture.md#gunplay))
- [x] Recoil is visible: the walked offset and the body's shot count reach the wire, so the drawn weapon sits where the pattern put it and every shot kicks the weapon, flashes the muzzle, and knocks the local camera ([view.ts](web/src/game/view.ts), [game-view-and-hud.md](docs/game/ui/game-view-and-hud.md#camera))
- [x] Move-spread interpolated from standing to moving by actual speed, drawn deterministically from the shooter and its shot count ([gunslinger.md](docs/game/design/gunslinger.md#gunplay), `World.spreadDegrees`)
- [x] Weight classes with recoil / spread / slowdown tradeoffs inside one damage band ([combat.json](data/tuning/combat.json), `World.handlingScale`; prediction applies the same multiplier)
- [x] The nine settled gun categories — pistol, revolver, SMG, shotgun, assault rifle, marksman, sniper, LMG, launcher ([gunslinger.md](docs/game/design/gunslinger.md#weapon-categories), [weapons.json](data/tuning/weapons.json))
- [x] Crafted special ammunition (rockets) as a finite, craftable resource: an ability cost kind that spends a carried material, with recipes in [ammunition.json](data/tuning/ammunition.json) and `World.CraftAmmunition` behind the same safe-zone gate
- [x] Snipers: hitscan to a weapon cap while scoped, then travel-time projectile with falloff and hard max range ([gunslinger.md](docs/game/design/gunslinger.md#snipers), `World.hitscan`)
- [x] Scope mode: peripheral blackout, view pushed toward the aim, server-side scoped state affecting movement, spread, and what the snapshot sends; camera exception in [game-view-and-hud.md](docs/game/ui/game-view-and-hud.md#camera)
- [x] Riot shield: frontal arc only, slows the user, locks fire, blocks bullets/projectiles, does not block a blast ([gunslinger.md](docs/game/design/gunslinger.md#defense), `World.blockedBy`)
- [x] Shield durability is the shield's health: every blocked round is charged to `guard.durability`, the overflow reaches the body, a spent shield breaks and drops, and it repairs only while lowered and returns only when whole (`World.guardAbsorb`, `World.stepGuard`, `Entity.shield`/`max_shield` on the wire and in the HUD)
- [x] Smoke deployable: a thrown canister leaves a standing field that casts a shadow like terrain (reworked in 2.6 from pure containment) — a body behind it is omitted from the snapshot, a body inside it sees only a small reveal circle — drawn as drifting particles ([deployable.go](server/internal/game/deployable.go), [visibility.go](server/internal/game/visibility.go), [gadgets.json](data/tuning/gadgets.json))
- [x] Flashbang: a thrown blast that takes vision whole for its window through a `blind` status, dealing no damage; a blinded body is sent nothing but itself and the client paints the blackout ([effects.json](data/tuning/effects.json), `World.blinded`)
- [x] Developer-mode level grant so content above the opening kit can be reached and exercised before mob XP lands — `POST /api/admin/progress`, bounded by `progression.admin_grant` ([administration.md](docs/administration.md))
- [x] Locked weapons, gadgets, and spells are listed disabled with the level that grants them, in both the Loadout and Crafting surfaces ([system-interfaces.md](docs/game/ui/system-interfaces.md#safe-zone-loadout-and-crafting))
- [x] Rare materials gate heavy weapons economically, never statistically: sniper, LMG, and launcher declare `requires_craft` and a material cost, so they have no stock configuration and must be built
- [x] Test: pattern determinism and recovery, the walked muzzle reaching the snapshot and settling back to aim, spread widening with speed, the pellet cone dividing one band hit, weight moving handling and never damage, the withheld-category gate, scope gating hitscan, falloff and hard range, shield arc and fire lock, shield durability breaking and recovering, finite ammunition, and blast reach ([gunplay_test.go](server/internal/game/gunplay_test.go)); a canister deploying where it lands and expiring, a concealing cloud casting a shadow — hiding a body behind it, keeping a body half out of its rim, revealing a body inside its reveal circle, letting a rim body peek out, and never shadowing the viewer's own rounds — a flashbang blinding without damaging and detonating exactly once where it lands, and a thrown gadget leaving the gun's pattern alone ([deployable_test.go](server/internal/game/deployable_test.go))

Phase 2.7 assigns every category to sustained, burst, or heavy-burst, with the
band owning both cadence and damage. The nine categories retain their handling,
range, magazine, scope, pellet, and blast distinctions within those bands.
Smoke and flashbangs did not wait for 2.6. Smoke is enforced by what the server
*sends* rather than by what the client draws, and 2.6 reworked it from its own
containment rule into a sight-blocker that casts a shadow like terrain — while
still never shadowing the viewer's own rounds — participating in the shared
automatic-target visibility check without pretending to be solid.

### 2.5 Mage kit
- [x] Author per-spell cooldowns and costs — the second resource axis alongside mana ([mage.md](docs/game/design/mage.md#mana-and-cooldowns)); every spell spends mana and every tier above one holds its own cooldown, each costing more and locking out longer than the tier below it ([abilities.json](data/tuning/abilities.json), `TestShippedSpellsPriceThemselves`)
- [x] Five elements as data and behaviour: Fire, Frost, Storm, Arcane, Earth ([mage.md](docs/game/design/mage.md#elements)) — each with four authored spells, its own secondary, and its own tint on bodies, rounds, telegraphs, and placed ground
- [x] Element secondaries: burn/DoT, light mitigation, blink-on-hit, shields/dispel/teleport, walls/knockback/armor ([effects.json](data/tuning/effects.json), [spell.go](server/internal/game/spell.go)); `armor` is the one new effect kind — mitigation with no pool, taken as the strongest rather than compounded
- [x] Author the settled 5 × 4 spell grid — all twenty rows, every element to tier 4 so affinity's 4 + 2 build is satisfiable ([spells.json](data/tuning/spells.json), `TestShippedSpellGridIsComplete`)
- [x] Stone wall: dynamic destructible collider, one per caster, placement rules, and lifetime carried in the rewind history ([wall.go](server/internal/game/wall.go), [mage.md](docs/game/design/mage.md#stone-wall)) — its ordinary world-item segments block movement, projectiles, and line of sight through the same collision geometry
- [x] Spell tiers 1–4 scaling mana, cooldown, telegraph, payoff, and whiff punishment; Phase 2.7 assigns their resolved damage to sustained, burst, or heavy-burst rather than authoring per-spell numbers
- [x] Mana crystals add element bias and area-shape behavior beyond projectile geometry (`area_radius` widens a blast, its field, and the telegraph that warns about both together; `element_damage` lifts one school only — [components.json](data/tuning/components.json), `crafting.Bias`)
- [x] Test: no spell delivers instant point-and-click damage ([spell_test.go](server/internal/game/spell_test.go)), plus placed fields pulsing, traps springing once, a zone closing in a stun, blinks stopping at cover, chains arcing, homing bounded by its turn rate, dispel stripping both sides, armor mitigating without being spent, and walls standing, blocking, replacing, expiring, and being refused inside safety or on top of an actor

Delivery grew six shapes on the one ability path — placement, pulsing fields, blink, chain, homing, and
dispel — rather than a branch per spell, so a Phase 4.3 mob that declares the same fields gets the same
behaviour. Rewind now resolves against the terrain that stood at the claimed moment, which the wall
needed and which also closed the pre-2.5 gap where a tree felled inside the rewind window disagreed with
the cover the shooter saw. Damage output comes from the three shared Phase 2.7 bands: what separates a
tier-1 spam spell from a tier-4 signature is its band, cost, cooldown, telegraph, and what it does to a
body. Rift repositions its caster only —
repositioning a squad waits on Phase 5 — and Bulwark's armor lands on its caster for the same reason.
Client prediction now mirrors the status layer as well as the equipped kit ([prediction.ts](web/src/game/prediction.ts),
`tuning.movementStatus`): a slow, root, or stun is predicted from the effect IDs already on the local
body's snapshot, and a knockback predicts nothing and lets reconciliation carry the body. Without it a
controlled player ran ahead of the server and was snapped back twenty times a second.

### 2.6 Line of sight
- [x] Vision/targeting occlusion: fixed walls and Mage-created wall segments block snapshot visibility and automatic acquisition when their entity attribute enables it; trees remain non-occluding landmarks ([visibility.go](server/internal/game/visibility.go), [architecture.md](docs/architecture.md#line-of-sight))
- [x] Shared substrate for smoke and for the Mage/Gunslinger LOS matchup: smoke and terrain sightlines feed the same targeting predicate, while manual ground placement remains exempt ([combat.md](docs/game/design/combat.md#time-to-kill))
- [x] GPU-computed client shadow wedges derived from snapshot colliders, with `occludes_vision` and `visible_in_shadow` entity attributes separating sight blockers from landmarks/decorations that remain readable beneath the 27% veil ([game-view-and-hud.md](docs/game/ui/game-view-and-hud.md#camera))
- [x] Rework: occlusion tests the whole silhouette (any part visible) rather than the centre; smoke casts a shadow like terrain with an inside-cloud reveal circle and rim peek instead of pure containment; area fields (firestorm/blizzard/cinder) and smoke are exempt from occlusion and always sent; the client fades entities in and out of sight instead of blinking them; occluders are collected once per send ([visibility.go](server/internal/game/visibility.go), [shadow.ts](web/src/game/shadow.ts), [view.ts](web/src/game/view.ts))
- [x] Fix: circular occluders — every smoke cloud and stone wall — cast no shadow on drivers where Pixi's default `mediump` fragment precision is fp16, because the quadratic disc test overflowed to a NaN that read as "no hit" while box terrain kept working. The veil shader now declares `precision highp float;` and measures the segment-to-centre distance instead of a discriminant ([shadow.ts](web/src/game/shadow.ts), [architecture.md](docs/architecture.md#line-of-sight))
- [x] Fix: fields were pinned in sight on the client, and the fade is the only collection path, so every cloud a player had ever seen leaked into the scene graph — invisible at the opacity its last delete frame left, still animated and re-batched every frame, which is what degraded the frame rate (and with it the measured ping) after repeated placements. Fields now follow the same present set as everything else, a sample buffer gone quiet far longer than the fade is collected outright as a backstop, and a puff is a tinted sprite off one shared disc texture so every field on screen batches into a single draw ([view.ts](web/src/game/view.ts), [architecture.md](docs/architecture.md#line-of-sight))

LOS is authoritative absence rather than a client mask: dynamic entities behind
solid cover are omitted from the viewer's snapshot, while terrain and every field
stay present for rendering and prediction. Occlusion is a property of the whole
body — a shoulder past a wall corner is seen. Smoke is a sight-blocker like
terrain but never shadows the viewer's own rounds and gives a body inside it only
a small reveal circle. Homing and chain acquisition use the same predicate, fall
through to the nearest visible target, and cannot select through cover or smoke.
Destroyed or expired terrain stops occluding immediately; its graceful fade is
feedback only. The client veils the hidden wedges and, inside smoke, the ground
past the reveal radius, and fades an entity out of sight rather than dropping it
outright — but authoritative absence remains the rule.

### 2.7 Roles and band enforcement
- [x] Cover the seven combat roles across both classes, declared on weapons, spells, and gadgets and enforced by tuning validation ([combat.md](docs/game/design/combat.md#shared-combat-roles))
- [x] Add the sustained / burst / heavy-burst damage bands to [combat.json](data/tuning/combat.json), each owning both its damage anchor and cadence ([combat.md](docs/game/design/combat.md#damage-bands))
- [x] Damage/DPS resolver computing per-hit damage, DPS, and raw TTK from band plus derived item data (`Tables.ResolveDamage`)
- [x] Automated Common-tier band test for every damaging weapon and spell ([pillars.md](docs/game/design/pillars.md#p2--vertical-progression-is-real-and-bounded))
- [x] Rarity tiers on components and materials, using the weakest component tier and applying its bounded multiplier once to the band anchor, never cadence ([progression-and-crafting.md](docs/game/design/progression-and-crafting.md#rarity-tiers))
- [x] Complete-recipe vertical-budget validation: damage ≤ ×1.45, effective health ≤ ×1.38, combined single-item power ≤ ×4/3, and no item below 2.0 s raw TTK ([progression-and-crafting.md](docs/game/design/progression-and-crafting.md#the-vertical-budget))

Phase 2.7 uses prototype Signature parts, a Signature prism/stave, an Aegis crystal, and Signature essence as explicit dummy catalog rows so weakest-tier rarity, horizontal modifiers, effective health, full-tier assembly, and boss-material costs are testable before Phase 3/4 acquisition systems land. Phase 2.7 originally shipped a class-locked keystone slot (Volatile focus and Thermal cycle) outside the six action bindings; it was removed as unnecessary, taking the `keystones.json` table, the wire field, and the heat/overcharge mechanics with it.

---

## Phase 3 — World axis

Phase 3 is where the world stops being a 3,000-unit test arena and becomes the expansive,
detailed environment the design promises. [3.0](#30-scale-substrate--prerequisites-pulled-forward-from-phase-8)
has shipped the substrate: the world is radius 45,000, indexed and chunked. Four decisions are settled and everything below
follows from them:

| Decision | Settled as |
|---|---|
| **Scale** | Radius **45,000** (×15 today). Straight-line foot crossing is 2:53 at 260 u/s; the ~5 minute target is met by a *real journey* — terrain detours, hostile territory, no direct route — not by dividing radius by walk speed. |
| **Detail** | **Layered procedural depth**. No bitmap art enters the play space; detail comes from a shader ground, batched procedural decals, and a parallax overhead layer, all clamped below the gameplay layer's contrast. |
| **Biomes** | **Procedural regions** from a world seed (noise/Voronoi), with load-time validation refusing any seed that leaves an element absent from a danger band. |
| **Editing** | Map editor edits the **authored overlay and per-region parameters**; the substrate stays deterministic from seed. The map document is versioned JSON, exported and imported whole. |

Sizing arithmetic, for reference when tuning: one "screen" is the AOI square, 2400 × 2400 =
5.76 M u². Radius 45,000 is 1,104 screens of area — 22 per player at 50 concurrent, 11 at 100.
Crampedness is not the risk at this scale; **emptiness is**, so nodes, outposts, routes, and
mob placement are what concentrate players and are load-bearing rather than decorative.

### 3.0 Scale substrate — prerequisites pulled forward from Phase 8

**Shipped.** At radius 45,000, MMO-plausible collider density (roughly one per 400 × 400) is tens of
thousands of world items, and the old flat slice was walked by every collision test, projectile step,
occluder collection, and per-viewer snapshot. Both are gone: one uniform grid answers every
broad-phase question, and terrain materialises per chunk around bodies
([architecture.md](docs/architecture.md#scale-the-spatial-index-and-chunk-residency)).

- [x] Uniform spatial grid (cell ≈ AOI half-extent, = `world.chunk_size`) indexing world items, players, projectiles, telegraphs, and deployables; one index answering collision, snapshot AOI, occluder collection, and target acquisition rather than a second visibility answer ([grid.go](server/internal/game/grid.go))
- [x] Chunked deterministic world-item generation: a chunk materialises from `(world_seed, chunk_coord)` on demand and is evicted when no body is near, so the world is never fully resident. Placement is a jittered lattice rather than rejection sampling, because a chunk cannot see its neighbours ([chunk.go](server/internal/game/chunk.go))
- [x] Chunk lifecycle does not violate existing contracts — a chunk holding a damaged or fading item is pinned, destruction leaves a scar that survives eviction, and authored fixtures, Mage walls, and developer spawns are never chunked at all. Chunks load before they can be seen and drop well outside interest, so the rewind window never spans a residency change
- [x] Raised `world.radius` to 45,000 and re-scaled the danger bands: hub 900, Fringe 9,000, Frontier 31,500, Deadlands 45,000 ([world.json](data/tuning/world.json))
- [x] Audited every radius-derived constant: spawn ring 600, terrain margins, developer-mode position bounds, and the client's world ring — now drawn as the arc facing the camera. The AOI half-extent deliberately did not move; it is a camera property rather than a world one
- [x] Load test at 50 and 100 concurrent bodies against the 64 KiB snapshot guardrail at the new terrain density: 11.0 KB and 15.0 KB largest snapshots, plus a spread-population test holding residency bounded per body ([chunk_test.go](server/internal/game/chunk_test.go))
- [x] [world.md](docs/game/design/world.md) carries the settled scale, the friction-based traversal target, and the procedural biome field
- [x] Phase 8's spatial-index and load-test entries moved here; deltas/compression and priority tiers remain in Phase 8

Left for the rest of Phase 3: the map editor, layered environment rendering, and world HUD.
Outposts, safety, and travel landed in [3.3](#33-outposts-safety-and-travel). The terrain scatter is now per-biome and the world is funnelled through ridge-belt
chokepoints ([3.2](#32-terrain-friction-and-traversal)).

### 3.1 World field — danger, biome, and grade by position

**Shipped.** The world stopped being a set of radius comparisons and became a field
([architecture.md](docs/architecture.md#the-world-field)).

- [x] One shared deterministic world-field module: `DangerAt`, `BiomeAt`, `GradeAt`, and `RegionAt` by position, derived from the world seed and region parameters, with bit-identical Go and TypeScript implementations checked against one fixture ([worldfield.go](server/internal/worldfield/worldfield.go), [worldfield.ts](web/src/game/worldfield.ts), `testdata/worldfield.json`)
- [x] Danger tiers as a first-class lookup replacing radius comparisons scattered through `World` — `World.DangerAt`/`Protected`/`Safe` now answer the loadout lock, the crafting gate, the wall placement rule, and both ends of PvP protection ([world.md](docs/game/design/world.md#radial-danger))
- [x] Procedural biome regions (noise-warped, radially compressed Voronoi) over the five elements, with a border blend the renderer cross-fades an ambient palette across ([biomes.json](data/tuning/biomes.json))
- [x] Material grade by radius on a **convex** reward curve, validated convex at load and cross-checked against each band's declared grade; its continuous `Richness` is the scalar Phase 4.1 yields will scale with ([world.md](docs/game/design/world.md#biomes-type--grade))
- [x] Universal structural, wood, and reagent materials everywhere; element-aligned and biome-growth materials gated to their region, with grade a ceiling rather than an equality — `Tables.MaterialsAt` ([materials.json](data/tuning/materials.json))
- [x] **Load-time coverage validation**: the loader samples the field across every band and refuses a seed that leaves a biome absent from one or below `field.coverage.minimum_share`. The shipped seed clears its 6% floor with a 15% worst case ([validate_field.go](server/internal/tuning/validate_field.go))
- [x] Test: the field is deterministic and reproduces every golden sample exactly in both languages; a degenerate seed is refused by name with the re-roll it needs
- [x] Content: 43 material rows across seven kinds, per-element focus and core crystals, reagent- and biome-gated gun parts, three new staves, and elemental rocket recipes — so a biome is worth travelling to and a build has a geography

The renderer tints the ground by biome and the HUD names the region, its band, and what
the ground yields. The chunk generator now reads the field for per-biome terrain
archetypes and belt barriers ([3.2](#32-terrain-friction-and-traversal)).

### 3.2 Terrain, friction, and traversal

The 5-minute target lives here. A straight-line 2:53 becomes a 5-minute journey only if the
straight line does not exist.

- [x] Per-biome terrain archetypes in [entities.json](data/tuning/entities.json): boulders, ridges, thickets, ice shelves/blocks, lava flows, fulgurites, mirror monoliths, cinder spires, salt crags — each declaring its own `occludes_vision` / `visible_in_shadow` attributes; the biome underfoot chooses the scatter and the barrier the ridge belts are built from ([architecture.md](docs/architecture.md#terrain-friction-and-traversal))
- [x] Macro structure: concentric impassable ridge belts broken by a handful of staggered passes, so radial travel is funnelled through chokepoints rather than crossing open ground — the belts fill a fine lattice with overlapping formations so they seal, and the passes stagger belt-to-belt so no straight radial line is clear ([terrain.go](server/internal/game/terrain.go))
- [x] Routes as the traversal reward: the scatter thins in each pass mouth so a chokepoint reads as an exposed, cleared lane. Phase 3.3 placed the outposts; the authored lanes that connect one to the next are overlay rather than substrate and move to the 3.4 map document
- [x] Density and placement rules per biome, generated per chunk from `(seed, chunk_coord)` and reproducible however chunks load and evict ([chunk.go](server/internal/game/chunk.go))
- [x] Test: shortest-path search across the live walkable grid reports a median on-foot journey to the rim of ≥ 5 minutes (318 s shipped, straight line ~170 s), and no straight radial line is clear from hub to rim ([terrain_test.go](server/internal/game/terrain_test.go))
- [x] Test: the walkable space is one connected region — every walkable cell, every biome, and the spawn ring are reachable from the hub, so nothing is sealed, stranded, or enclosed ([terrain_test.go](server/internal/game/terrain_test.go))

### 3.3 Outposts, safety, and travel

- [x] Populated [outposts.json](data/tuning/outposts.json) with five rows across the Fringe and Frontier and none in the Deadlands, placed in the walkable annuli between the ridge belts and enforced by validation ([world.md](docs/game/design/world.md#outposts-and-travel)); terrain generation defers to each footprint, so an outpost is never sealed inside a belt
- [x] Discovery trigger on proximity (`World.stepDiscovery`), awarding the `discovery` XP source already priced in [progression.json](data/tuning/progression.json) and persisting the unlock immediately through `World.DrainDirtyState`/`Engine.drainDirtyStateLocked`, as loadout commits do
- [x] Per-outpost no-PvP radius replacing radius-from-origin protection: `World.Protected`/`Safe` overlay the outpost bubbles on the world field, and the gates on `SetLoadout`, `Craft`, `CraftAmmunition`, and `CraftRideable` resolve through `World.serviceAt` ([outpost.go](server/internal/game/outpost.go), [world.md](docs/game/design/world.md#outpost-safety))
- [x] Outpost services declared per row — loadout, crafting, respawn — so a forward outpost offers less than the hub, mirrored on the client by [outposts.ts](web/src/game/outposts.ts)
- [x] Exit invulnerability on `Player`, granted on the protected→unprotected crossing and broken by the player's own hostile use, reported through the existing `Entity.invulnerable` so attackers see it too
- [x] **Changed from the original plan:** mounts are *rideable entities*, not a movement state. A Mage crafts a summoning crystal that materialises a horse; a Gunslinger crafts a motorcycle. Both appear in the world when crafted, are transport only (no weapons or spells while riding), carry their own health, take the damage aimed at their rider, force a dismount when destroyed, and cannot be mounted for a lockout window after combat ([rideable.go](server/internal/game/rideable.go), [rideables.json](data/tuning/rideables.json))
- [x] **Changed from the original plan:** fast travel is banned outright rather than only while carrying materials. There is no voluntary teleport between outposts at all, and respawn is a forced relocation rather than a destination menu
- [x] Recall and respawn destinations resolve against discovered outposts through one shared `nearestUnlockedOutpost`, replacing the always-the-hub fallback. **Changed from the original plan:** a death returns the player to the *nearest* unlocked outpost with no choice, superseding the free-choice respawn menu in [economy-death-and-pve.md](docs/game/design/economy-death-and-pve.md#respawn), which is updated to match. Respawn stays instant; the ~5 s timer remains Phase 4.2
- [x] Test: protection follows outposts rather than the origin, services gate per row, discovery unlocks and awards exactly once, exit invulnerability covers the crossing and ends on the player's own shot, a Deadlands death respawns at the nearest Frontier outpost, rides are built only at a crafting outpost and replace one another, a mounted body moves at the ride's speed and cannot fight, and destroying a ride dismounts its rider unhurt ([outpost_test.go](server/internal/game/outpost_test.go), [rideable_test.go](server/internal/game/rideable_test.go), [outposts.test.ts](web/src/game/outposts.test.ts))

Three rules were deliberately changed from what this file and the design documents
originally specified, and the documents were rewritten rather than left to drift:
mounts/vehicles are entities rather than a movement state, death respawns at the
nearest unlocked outpost with no menu, and fast travel is banned entirely. The
Phase 3.2 note about outpost-to-outpost lanes now has its outposts; laying the
lanes themselves along the pass mouths remains open and moves to 3.4's map
document, since a route is authored overlay rather than generated substrate.

### 3.4 Map editor and the map document

- [ ] Versioned map-document schema: world seed, region parameter sets, danger-band radii, outposts, POIs, routes, and authored fixtures — references and parameters only, never materialised substrate
- [ ] `POST /api/admin/map/export` and `/import` behind the existing admin wrapper, with import validating the full document — including biome coverage — before anything is applied ([administration.md](docs/administration.md))
- [ ] Admin map editor as a new Field-menu surface: a zoomed-out world view with biome field preview, band rings, and placement/selection of outposts, POIs, routes, and fixtures, reusing the existing pointer spawn/select/edit/delete tooling ([game-menu.md](docs/game/ui/game-menu.md))
- [ ] Seed re-roll with live coverage validation, so a refused seed is visibly refused in the editor rather than at server start
- [ ] Live reload of an imported map without a process restart, re-materialising chunks and re-validating every placed body's position against the new geometry
- [ ] Test: export → import round-trips to an identical world; a document failing coverage or geometry validation is refused atomically with a named reason

### 3.5 Layered environment rendering

Amends [visual-direction.md](docs/game/design/visual-direction.md), which currently says atmosphere
comes from palette *rather than* detail. The replacement rule is a **layered detail budget**:
detail lives below the gameplay layer, contrast lives in it.

- [ ] Amend [visual-direction.md](docs/game/design/visual-direction.md) with the layer model and the readability floor, keeping the procedural boundary (no bitmap art in the play space) intact
- [ ] **L0 ground** — multi-octave noise fragment shader on one quad: biome-blended colour, macro variation, flow, cracks, moisture. Replaces the flat fill and grid at [view.ts:86-94](web/src/game/view.ts#L86-L94)
- [ ] **L1 decals** — procedural scatter (tufts, drifts, fractures, roots) as instanced sprites off runtime-generated shared textures, batching into a single draw. Use the technique the Phase 2.6 smoke fix established; never emit per-instance `Graphics`
- [ ] **L2 terrain** — the 3.2 colliders, drawn with the existing flat-fill-plus-outline vocabulary
- [ ] **L3 gameplay** — actors, projectiles, telegraphs, unchanged and at maximum contrast
- [ ] **L4 overhead** — parallax canopy, cloud shadows, and per-biome ambient particles (embers, snow, dust) at partial alpha, above the shadow veil
- [ ] Ambient palette shift by biome and danger across all three channels — background value/tint, grid colour/opacity, ambient saturation ([visual-direction.md](docs/game/design/visual-direction.md#palette))
- [ ] **Readability floor**: L0–L1 clamped to a bounded value and saturation range so L3 always wins contrast, enforced as a checked invariant rather than an art guideline
- [ ] Distinct safe-zone ambient so the loadout-lock boundary is *seen*, not just stated
- [ ] Test: the readability floor holds for every biome × danger combination; environment layer count and draw calls stay bounded as detail density rises; frame budget holds at maximum density with a full AOI of entities

### 3.6 World information and HUD

- [ ] Location readout: region name, biome, and danger band, updating on crossing rather than continuously ([game-view-and-hud.md](docs/game/ui/game-view-and-hud.md#safety-and-danger))
- [ ] Visible no-PvP boundary around the nearest outpost, and exit-invulnerability self state with remaining duration
- [ ] Boundary-crossing announcements that teach once and condense to persistent state (shared with Phase 7)
- [ ] ⚠ **Resolve the minimap/compass decision** ([ui/open-decisions.md](docs/game/ui/open-decisions.md)) — deferred while the world was 11 seconds across; at radius 45,000 a player cannot navigate or find a discovered outpost without it, so it now blocks this section
- [ ] Discovered-outpost markers, with undiscovered outposts never rendered as locked rows ([system-interfaces.md](docs/game/ui/system-interfaces.md#death-and-respawn))

Phase 4.1 harvest nodes and Phase 4.3 mob placement consume 3.1's field and 3.2's chokepoints
directly; both are what make a 1,104-screen world feel inhabited rather than empty, so neither
should be tuned against the old arena scale. Phase 6 inherits L0–L4 as the layer contract its
palette module and scatter props fill in.

---

## Phase 4 — Loop axis

### 4.1 Materials & harvesting
- [x] Carried-material inventory on the player, distinct from crafted items — carried materials are spendable and droppable, crafted items are permanent (Phase 2.3); crafted special ammunition (Phase 2.4) lives in the same inventory and is therefore droppable too
- [ ] Harvest nodes as world entities; grade-scaled channel time ([economy-death-and-pve.md](docs/game/design/economy-death-and-pve.md#resource-sources))
- [ ] Interact/channel state machine, interruptible at any time
- [ ] Contextual harvest prompt and interruption feedback ([game-view-and-hud.md](docs/game/ui/game-view-and-hud.md#contextual-prompts-and-feedback))
- [ ] HUD carried-materials module with danger/insurance consequence, stronger outside safety

### 4.2 Death, drops, and respawn
- [ ] Death penalty: keep crafted gear, drop most carried raw materials at the death location ([economy-death-and-pve.md](docs/game/design/economy-death-and-pve.md#death))
- [ ] Danger-tier-scaled insurance that never scales with squad size
- [ ] Ground-drop world entity: 30 s killer-squad exclusivity, then free-for-all, 15 min despawn
- [ ] Pickup is unrestricted by unlocks; crafting still enforces blueprint requirements
- [ ] ~5 s respawn timer; Phase 3.3 already replaced the origin respawn with a relocation to the nearest unlocked outpost, so only the timer is left
- [x] Destination resolution: Phase 3.3 settled this as *nearest unlocked outpost, no choice* rather than a free-travel menu, because a destination menu makes dying a routing decision and there is no fast travel ([economy-death-and-pve.md](docs/game/design/economy-death-and-pve.md#respawn))
- [ ] Death summary UI: kept gear, insured materials, dropped materials, death location, the outpost being returned to with its distance and danger band, and a visible countdown ([system-interfaces.md](docs/game/ui/system-interfaces.md#death-and-respawn))

### 4.3 Mobs
- [ ] Mob entity type, AI tick in the world step, and a per-class behaviour contract ([economy-death-and-pve.md](docs/game/design/economy-death-and-pve.md#enemy-classes))
- [ ] Sentry: fused body + independently rotating turret, patrol, aggro-nearest, drive-to-preferred-range, leash and disengage
- [ ] Telegraphed dodgeable projectile shots — never hitscan
- [ ] Data-driven variants from the start: turret count, movement, cadence, telegraph speed per tier
- [ ] Placement at chokepoints and node clusters
- [ ] Mob render path with biome tint, hostile outline/ring, tier-readable turret silhouette
- [ ] Set aggro radius, leash, reset, speed, and cadence values at implementation, then fix by playtesting

### 4.4 Progression pacing
- [ ] Award XP from kills, harvest, and discovery
- [ ] Tune drop rates against the pacing targets: ~1 h to a coherent build, ~10 h to rim viability ([progression-and-crafting.md](docs/game/design/progression-and-crafting.md#progression-pacing))

---

## Phase 5 — Social axis

- [ ] Squad formation with a hard cap of four; squad ID on players ([squads-and-world-bosses.md](docs/game/design/squads-and-world-bosses.md#squads))
- [ ] Safe-zone-set loot rule: free-for-all vs. shared, unchangeable in the field
- [ ] Shared loot splits among nearby members; free-for-all uses the 30 s squad priority window
- [ ] Squad-aware rendering: outline and overhead ring for self / squad / neutral / hostile ([view.ts:120-124](web/src/game/view.ts#L120-L124) distinguishes only self vs. hostile)
- [ ] Squad HUD roster: up to four members with health, state, direction, and distance
- [ ] World bosses: large biome-themed procedural constructs with region-scaled difficulty
- [ ] Squad-pooled contribution; rare loot to the top four split by contribution ratio
- [ ] Participation floor as a percentage of boss health
- [ ] Boss UI: pooled contribution explained, authoritative progress, rare-ranking vs. participation distinguished, support play never framed as underperformance ([system-interfaces.md](docs/game/ui/system-interfaces.md#squads-loot-and-bosses))
- [ ] Drop feedback distinguishes squad-exclusive / free-for-all / despawning without colour alone

---

## Phase 6 — Presentation & readability

- [ ] Shared palette module as the single source of element hues; colours are ad-hoc literals today ([view.ts:10-13](web/src/game/view.ts#L10-L13))
- [ ] Procedural weapon silhouettes generated from the crafting recipe, not hardcoded shapes ([view.ts:114-126](web/src/game/view.ts#L114-L126))
- [ ] Gun silhouette reveals weight class, likely range, and rate of fire ([classes.md](docs/game/design/classes.md#build-visibility))
- [ ] Mage body and staff tinted by dominant element; body is a fixed orange today
- [ ] Deployables and heavy swaps appear only when used, preserving the visibility asymmetry
- [ ] Projectile type → silhouette table: bullets, sniper rounds, spell bolts, area seeds, thrown gadgets
- [ ] Overhead threat/squad ring as the required redundant non-hue cue
- [ ] Feedback: damage flash on the struck body, mana bar in-world, procedural death burst (death only fades to 32 % alpha, [view.ts:106](web/src/game/view.ts#L106))
- [ ] Effect layer: muzzle flashes, impact rings, explosion bursts, recoil/scale pops, restrained camera feedback, sparing glow
- [ ] Biome scatter props and a visually distinct safe zone so the loadout-lock boundary is *seen*, not just stated ([visual-direction.md](docs/game/design/visual-direction.md#world-rendering))
- [ ] Reconcile hitbox honesty: radius-20 collision ([world.go:38](server/internal/game/world.go#L38)) vs. the ~35×33 drawn Gunslinger ([view.ts:124](web/src/game/view.ts#L124))
- [ ] Colourblind redundancy audit as a per-visual checklist, enforced on every new visual rather than retrofitted
- [ ] ⚠ Validate final element hues against real fights ([open-decisions.md](docs/game/design/open-decisions.md)) — build the palette swappable and ship before this resolves
- [ ] Remove the line telegraph from the Mage's ordinary bolt cast; replace it with a glow on the staff's mana crystal, tinted by the spell's element, as the sole windup cue ([telegraph.ts](web/src/game/telegraph.ts))
- [ ] Ground-placed spells (fields, walls, other placement deliveries) get a magic-circle telegraph drawn on the ground at the resolved location instead of the generic shape telegraph

---

## Phase 7 — Interface, accessibility, and platform

- [x] Fixed dual-stick mobile combat controls with independent multi-touch capture, direct world-touch firing, and reachable dash/interact/reload/scope placement ([responsive-and-mobile.md](docs/game/ui/responsive-and-mobile.md#gameplay-and-touch))
- [x] Reliable touch-pointer activation for menu/connection/respawn actions, plus iOS gameplay selection/callout suppression without disabling form editing
- [x] Stabilize hotbar nodes across snapshot redraws; add pointer-up activation for menu tabs/hotbar, touch-only manual menu minimization, and a fixed non-zooming iOS viewport
- [x] Snapshot interest coverage includes the full square of the configured maximum view distance, including both camera corners ([game-view-and-hud.md](docs/game/ui/game-view-and-hud.md#camera))
- [ ] Actor plates: display name, health, and squad/threat marker with density-based simplification and the specified occlusion priority ([game-view-and-hud.md](docs/game/ui/game-view-and-hud.md#actor-labels))
- [ ] Enemy Mage mana stays private
- [ ] Lower-centre ability bar: slots, cooldowns, charges, class resource, bindings, universal dash
- [ ] Conditional collapsing HUD modules that never hide local health, critical resources, or menu access
- [ ] Contextual prompts in one consistent interaction area covering harvest, loot priority, safe-zone transitions, and actionable failures
- [ ] Boundary-crossing announcements that teach once and then condense to persistent state
- [ ] Menu sections still missing: Squad and Activity ([game-menu.md](docs/game/ui/game-menu.md#information-architecture)); Crafting and Inventory landed with Phase 2.3
- [ ] Exit game: explain vulnerability/material/position consequences and confirm when leaving creates risk ([game-menu.md](docs/game/ui/game-menu.md#exit-and-session-actions)) — still a bare `confirm()` ([main.ts:73](web/src/main.ts#L73)), though it now states the logout linger
- [ ] Shared network states on every remote-data surface: loading, empty, unavailable/locked, error, stale/conflict, offline/reconnecting ([shared-states.md](docs/game/ui/shared-states.md))
- [ ] Idempotent retries; success shown only after server confirmation; never claim an unconfirmed spend, craft, pickup, or loadout change
- [ ] Account overlays: focus trap and restore, Escape handling, pending-submission state, duplicate rejection, field vs. form error placement ([home.md](docs/game/ui/home.md#account-flows))
- [ ] Password recovery, email verification, and expired-session reauthentication
- [ ] Reconnect grace period and a visibly inactive last-frame backdrop during recovery ([connection-and-recovery.md](docs/game/ui/connection-and-recovery.md))
- [ ] Remappable gameplay controls
- [ ] Full keyboard navigation, visible focus, semantic names, announced validation ([accessibility.md](docs/game/ui/accessibility.md))
- [ ] Text/UI contrast verified over every biome and danger palette
- [ ] Independent audio controls and non-audio critical cues
- [ ] Mobile: configurable control regions, idle fade with discoverable return, collapsed squad/activity summaries ([responsive-and-mobile.md](docs/game/ui/responsive-and-mobile.md#gameplay-and-touch))
- [ ] Optimise the touch layout for six equipped slots: the current plain button row above the controls works but is not placed for one-handed reach, and it competes with the aim and movement zones ([game-view-and-hud.md](docs/game/ui/game-view-and-hud.md#slot-selection))

---

## Phase 8 — Scale & operations

- [ ] Snapshot deltas and an enforced bandwidth budget
- [ ] Priority tiers so a dense fight degrades by relevance rather than uniformly
- [ ] Versioned welcome/tuning message so simulation constants can move without desyncing client prediction
- [ ] Rate-limit authentication endpoints; document the trusted-origin policy for split-host deployments

The spatial index and the 100+ concurrent load test moved to [Phase 3.0](#30-scale-substrate--prerequisites-pulled-forward-from-phase-8) and shipped there: at radius 45,000 the linear per-client scan could not survive terrain density, so they were prerequisites rather than scaling work.

---

## Open decisions

Blocking work is tracked with **⚠** above. Resolve each in its owning document, then delete it here.

| Area | Owner | Blocks |
|---|---|---|
| Colourblind-validated palette | [design/open-decisions.md](docs/game/design/open-decisions.md) | Phase 6 palette module — ship it swappable and unblock |
| Guest play, character slots, naming, first-time class choice | [ui/open-decisions.md](docs/game/ui/open-decisions.md) | Home play panel |
| Minimap, compass, and permissible world information | [ui/open-decisions.md](docs/game/ui/open-decisions.md) | **⚠ Phase 3.6** — now blocking: a radius-45,000 world is not navigable without it |
| Floating combat text and target inspection | [ui/open-decisions.md](docs/game/ui/open-decisions.md) | Phase 6 feedback |
| Logout/recall *presentation* — the rule itself is settled in [economy-death-and-pve.md](docs/game/design/economy-death-and-pve.md#logging-out) | [ui/open-decisions.md](docs/game/ui/open-decisions.md) | Phase 7 exit and reconnect |
| Mobile orientation, touch model, assistance limits | [ui/open-decisions.md](docs/game/ui/open-decisions.md) | Phase 7 mobile |
| Controller support, live-combat screen-reader scope, high-contrast mode | [ui/open-decisions.md](docs/game/ui/open-decisions.md) | Phase 7 accessibility |

Already resolved and now tracked as implementation work above: outpost safety, respawn rules, Sentry
values and variants, Mage ranged poke, starter kit, progression pacing, and renderer choice.

## Deferred by choice

Not gaps. Listed so they are not mistaken for oversights ([design/open-decisions.md](docs/game/design/open-decisions.md#deferred-systems),
[ui/open-decisions.md](docs/game/ui/open-decisions.md)):

- Netcode beyond the single-process foundation; horizontal world sharding
- Monetisation and cosmetics; marketplace and player trading
- Guilds, territory, and outpost capture
- Onboarding and tutorials
- Mob classes beyond the Sentry
- The full numeric balance pass
