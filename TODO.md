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

Biome-placement rows are intentionally empty until Phase 3; component and material rows landed with Phase 2.3. The Sentry row carries its settled contract without the values [economy-death-and-pve.md](docs/game/design/economy-death-and-pve.md#sentry) defers to implementation. Runtime table delivery to a live client stays in Phase 8.

### 1.2 Persistence & migration
- [x] Read `schema_version` and run sequential forward migrations — `PRAGMA user_version` for the database schema, `characters.schema_version` for the record shape ([sqlite.go](server/internal/store/sqlite.go), [model.go](server/internal/model/model.go), [architecture.md](docs/architecture.md#persistence-and-migration))
- [x] Retired-ID → replacement/refund resolution map; never delete an ID ([retired.json](data/tuning/retired.json), `Tables.Resolve` in [tuning.go](server/internal/tuning/tuning.go), [invariants.md](docs/game/design/invariants.md))
- [x] Persist character position, carried inventory, and unlocked outposts; saved every 15 s, at logout, and at shutdown, restored and re-validated on join ([engine.go](server/internal/game/engine.go), [world.go](server/internal/game/world.go))
- [x] Store crafted items as weapon + component IDs, never a stat snapshot (`crafted_items`; `model.CraftedItem`); Phase 2.3 is what writes them
- [x] 10 s logout linger: the body stays in the world, killable and unable to act, so disconnecting is not an escape ([session.json](data/tuning/session.json), [engine.go](server/internal/game/engine.go))
- [x] Saved position expires after 30 min offline and recalls to the nearest unlocked outpost or the hub ([outposts.json](data/tuning/outposts.json), `World.recallDestination`)
- [x] One body per account: a second character's join is refused while the first is in the world, lingering included, so switching characters is not a combat-log escape (`Engine.Join`/`ErrAccountInWorld`, [architecture.md](docs/architecture.md#one-body-per-account))

Unlocked outposts round-trip through the world but nothing mutates them yet — outpost discovery is Phase 3. Carried materials are now spent by Phase 2.3 crafting; harvesting, which is what legitimately produces them, is Phase 4.1. `outposts.json` ships empty, so every recall resolves to the hub until Phase 3 places them. Lingering bodies are now flagged on the wire and rendered as dimmed, offline actors; Phase 7 still owns the surrounding exit and reconnect UX.

### 1.3 Server-side ability/effect framework
- [x] One authoritative ability system replacing the ad-hoc branches in `stepPlayer`/`tryFire` — `World.ability`/`useAbility`/`spend`/`deliver` ([ability.go](server/internal/game/ability.go), [architecture.md](docs/architecture.md#abilities-and-effects))
- [x] Ability schema declares cost, cooldown, telegraph, and **a mandatory dodge vector**; weapons and spells hold identity and reference it ([abilities.json](data/tuning/abilities.json), [tuning.go](server/internal/tuning/tuning.go), [invariants.md](docs/game/design/invariants.md))
- [x] Status-effect layer: burn/DoT, slow, root, stun, knockback, shields ([effects.go](server/internal/game/effects.go), [effects_test.go](server/internal/game/effects_test.go))
- [x] Reject at load any damaging ability with no declared dodge vector — and any that claims one the simulation does not deliver ([validate.go](server/internal/tuning/validate.go))

`effects.json` carries one shipped row — the knockback a rocket blast applies. The layer runs all six
kinds, but no design document has settled a magnitude for the other five, so Phase 2.5's element
secondaries author those rows and the tests exercise the layer against rows they add themselves. Active effect IDs already reach the client in
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
is not a way to respec. Keystone slots wait on Phase 2.7. The equippable set is narrowed from "every
live row" to "what this character owns" by the Phase 2.2 unlock ledger.

### 2.2 Unlock ledger & starter kit
- [x] Flat permanent unlock ledger for gun parts, spells, and keystone IDs — sorted content IDs on the character record, never shortened ([model.go](server/internal/model/model.go), [progression.go](server/internal/progression/progression.go), [architecture.md](docs/architecture.md#progression-and-unlocks))
- [x] XP sources and a level → unlock mapping: [progression.json](data/tuning/progression.json) prices the sources and holds the curve, while each weapon/spell/gadget row declares its own `unlock_level`, so `Tables.UnlocksThrough` derives the mapping with no second table to keep in step
- [x] Starter kit on creation: one random basic class weapon + a random draw of low-tier unlocks, seeded from the character ID ([progression-and-crafting.md](docs/game/design/progression-and-crafting.md#starter-kit), `progression.StarterKit`, [api.go](server/internal/api/api.go))
- [x] Level now drives something: `World.creditKill` awards `player_kill` XP to the [damage-credited](docs/architecture.md#damage-attribution-and-combat-log) killer, `progression.Advance` grants what the levels crossed unlock, and the engine persists and pushes the change as `SERVER_PROGRESS`
- [x] `loadout.Equippable`/`Validate`/`Resolve` intersect with the ledger, so unowned content is refused on the mutation path rather than merely hidden by the menu
- [x] Test: a zero-material character can fill a coherent loadout immediately ([progression_test.go](server/internal/progression/progression_test.go)), plus starter-kit stability/randomness, level grants, ledger-gated validation, and the separate progression save path

Only `player_kill` has a trigger: mob kills, harvesting, and outpost discovery are priced in the table and awarded by Phases 4.3, 4.1, and 3, and Phase 4.4 tunes the curve against the pacing targets. Keystone IDs share the ledger's shape but have no rows until Phase 2.7. Phase 2.4 widened the Gunslinger's basic set to four categories and its gadget pool to the riot shield, smoke, and flashbangs without touching the draw; a developer-mode level grant now reaches the rest of the ledger before mob XP lands; the Mage's is still one staff and one spell until Phase 2.5.

### 2.3 Slotted-blueprint crafting
- [x] Blueprint + slot + component definitions with material costs and **behavioural** (not power) effects ([components.json](data/tuning/components.json), [crafting.go](server/internal/crafting/crafting.go), [progression-and-crafting.md](docs/game/design/progression-and-crafting.md#slotted-blueprint-crafting))
- [x] Gun slots: muzzle, barrel, scope, trigger, magazine — two options each, costed in structural materials so geography never hard-locks a Gunslinger
- [x] Staff slots: core, focus, conduit — element-aligned shards price the element-typed parts
- [x] Crafting gated to safe zones; raw materials must be hauled there (`World.Craft`/`ErrCraftingLocked`, [architecture.md](docs/architecture.md#slotted-blueprint-crafting))
- [x] Crafting UI: blueprint, slots, compatible components, owned/required materials with shortfalls, plain-language behaviour changes, spend confirmation, rejection outcomes ([system-interfaces.md](docs/game/ui/system-interfaces.md#safe-zone-loadout-and-crafting), [crafting.ts](web/src/game/crafting.ts), [main.ts](web/src/main.ts))
- [x] Crafted items are equippable: the weapon slot names either a stock row or an instance, resolved through `crafting.Inventory.Equipped` on the same path
- [x] Test: recipe legality, cost aggregation, per-material shortfalls, atomic refusals, modifier application, the shared projectile row surviving unmutated, items round-tripping through a rejoin, and the inventory capacity

Components declare an open `modifiers` map, but only over attributes the simulation reads, and the loader
rejects `interval_ms` outright: fire cadence is the DPS axis, and crafting changes handling and ceiling.
Recoil, spread, and scoped movement joined the vocabulary with Phase 2.4 and the gun components now use them.
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
- [x] Smoke deployable: a thrown canister leaves a standing field that hides anything across it from the snapshot — not from the renderer — while revealing bodies close enough to touch, drawn as drifting particles ([deployable.go](server/internal/game/deployable.go), `World.occluded`, [gadgets.json](data/tuning/gadgets.json))
- [x] Flashbang: a thrown blast that takes vision whole for its window through a `blind` status, dealing no damage; a blinded body is sent nothing but itself and the client paints the blackout ([effects.json](data/tuning/effects.json), `World.blinded`)
- [x] Developer-mode level grant so content above the opening kit can be reached and exercised before mob XP lands — `POST /api/admin/progress`, bounded by `progression.admin_grant` ([administration.md](docs/administration.md))
- [x] Locked weapons, gadgets, and spells are listed disabled with the level that grants them, in both the Loadout and Crafting surfaces ([system-interfaces.md](docs/game/ui/system-interfaces.md#safe-zone-loadout-and-crafting))
- [x] Rare materials gate heavy weapons economically, never statistically: sniper, LMG, and launcher declare `requires_craft` and a material cost, so they have no stock configuration and must be built
- [x] Test: pattern determinism and recovery, the walked muzzle reaching the snapshot and settling back to aim, spread widening with speed, the pellet cone dividing one band hit, weight moving handling and never damage, the withheld-category gate, scope gating hitscan, falloff and hard range, shield arc and fire lock, finite ammunition, and blast reach ([gunplay_test.go](server/internal/game/gunplay_test.go)); a canister deploying where it lands and expiring, smoke hiding what is behind it but not what is touching it, a flashbang blinding without damaging, and a thrown gadget leaving the gun's pattern alone ([deployable_test.go](server/internal/game/deployable_test.go))

Every category shares the `standard` band and fires no faster than the 300 ms
baseline, because with one band cadence *is* DPS. Rate-of-fire identity — the
SMG's spray, the sniper's single heavy shot — waits on Phase 2.7's sustained,
burst, and heavy-burst bands; what separates the nine today is handling.
Smoke and flashbangs did not wait for 2.6. Smoke carries its own narrow rule —
a cloud on the segment between two bodies hides them from each other, and both
are enforced by what the server *sends* rather than by what the client draws —
which is the case a canister is bought for and nothing more. General line of
sight through cover and walls is still Phase 2.6's substrate, and the Mage's
stone wall sequences after it.

### 2.5 Mage kit
- [ ] Author per-spell cooldowns and costs — the second resource axis alongside mana ([mage.md](docs/game/design/mage.md#mana-and-cooldowns)); 1.3 enforces both from `abilities.json`, and every shipped row still declares a zero cooldown
- [ ] Five elements as data and behaviour: Fire, Frost, Storm, Arcane, Earth ([mage.md](docs/game/design/mage.md#elements))
- [ ] Element secondaries: burn/DoT, light mitigation, blink-on-hit, shields/dispel/teleport, walls/knockback/armor
- [ ] Author the settled 5 × 4 spell grid — all twenty rows, every element to tier 4 so affinity's 4 + 2 build is satisfiable ([mage.md](docs/game/design/mage.md#the-spell-grid))
- [ ] Stone wall: dynamic destructible collider, one per caster, placement rules, and lifetime carried in the rewind history ([mage.md](docs/game/design/mage.md#stone-wall)) — common entity and box-collision substrate exists; sequence behavior after 2.6 so it ships blocking line of sight
- [ ] Spell tiers 1–4 scaling mana, cooldown, telegraph, payoff, and whiff punishment
- [ ] Staff components alter element bias and cast/area shape beyond projectile geometry; cast speed, mana cost, and projectile shape already run through Phase 2.3's modifiers
- [ ] Test: no spell delivers instant point-and-click damage

### 2.6 Line of sight
- [ ] Vision/targeting occlusion; trees currently block projectiles only
- [ ] Substrate for smoke and for the Mage/Gunslinger LOS matchup ([combat.md](docs/game/design/combat.md#time-to-kill))

### 2.7 Roles, keystones, and band enforcement
- [ ] Cover the seven combat roles across both classes; only Damage exists ([combat.md](docs/game/design/combat.md#shared-combat-roles))
- [ ] Keystones: behaviour-changing tradeoffs (empowered-but-costlier casts, overheat-instead-of-reload)
- [ ] Add the sustained / burst / heavy-burst damage bands to [combat.json](data/tuning/combat.json); only `standard` exists, and one band cannot carry both a shotgun and a rifle ([combat.md](docs/game/design/combat.md#damage-bands)) — Phase 2.4's nine categories all point at `standard` and are separated by handling alone until this lands
- [ ] Damage/DPS resolver computing from item data instead of one constant
- [ ] Automated band test: every weapon and spell lands inside the effective damage band ([pillars.md](docs/game/design/pillars.md#p2--vertical-progression-stays-compressed))

---

## Phase 3 — World axis

- [ ] Danger tiers as a first-class lookup — hub / Fringe T1 / Frontier T2 / Deadlands T3; only two bands exist as geometry ([world.md](docs/game/design/world.md#radial-danger))
- [ ] Material grade by radius, with a convex reward curve pulling veterans to the rim
- [ ] Biome map crossed with radius (type × grade), plus a biome lookup by position ([world.md](docs/game/design/world.md#biomes-type--grade))
- [ ] Universal structural materials everywhere; element-aligned materials biome-gated
- [ ] Multiple outposts with services; discover-to-unlock ([world.md](docs/game/design/world.md#outposts-and-travel)) — the [outposts table](data/tuning/outposts.json) and the per-character unlocked list already exist and need rows plus a discovery trigger
- [ ] Per-outpost no-PvP radius replacing radius-from-origin protection ([world.md](docs/game/design/world.md#outpost-safety))
- [ ] Exit invulnerability state on `Player`, broken by the player's own hostile action, with a protocol field so attackers see it too
- [ ] Mounts/vehicles and the "no fast travel while carrying raw materials" rule
- [ ] HUD: visible no-PvP boundary, exit-invulnerability self state with remaining duration, location/biome/danger-band readout ([game-view-and-hud.md](docs/game/ui/game-view-and-hud.md#safety-and-danger))
- [ ] Ambient palette shift by biome and danger — background value/tint, grid colour/opacity, saturation ([visual-direction.md](docs/game/design/visual-direction.md#palette)); all three channels are static today ([view.ts:86-94](web/src/game/view.ts#L86-L94))

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
- [ ] ~5 s respawn timer replacing instant origin respawn ([world.go:134-144](server/internal/game/world.go#L134-L144))
- [ ] Free destination choice among discovered outposts plus the hub, nearest preselected, timer expiry falls back to it ([economy-death-and-pve.md](docs/game/design/economy-death-and-pve.md#respawn))
- [ ] Death summary UI: kept gear, insured materials, dropped materials, death location, scannable destination list with distance and danger band, visible countdown ([system-interfaces.md](docs/game/ui/system-interfaces.md#death-and-respawn))
- [ ] Undiscovered outposts are never rendered as locked rows

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

- [ ] Spatial hash or quadtree replacing the O(players + projectiles + telegraphs + world items) per-client snapshot path ([snapshot.go](server/internal/game/snapshot.go))
- [ ] Snapshot deltas and an enforced bandwidth budget
- [ ] Load test against the 100+ concurrent design target ([world.md](docs/game/design/world.md))
- [ ] Versioned welcome/tuning message so simulation constants can move without desyncing client prediction
- [ ] Rate-limit authentication endpoints; document the trusted-origin policy for split-host deployments

---

## Open decisions

Blocking work is tracked with **⚠** above. Resolve each in its owning document, then delete it here.

| Area | Owner | Blocks |
|---|---|---|
| Colourblind-validated palette | [design/open-decisions.md](docs/game/design/open-decisions.md) | Phase 6 palette module — ship it swappable and unblock |
| Guest play, character slots, naming, first-time class choice | [ui/open-decisions.md](docs/game/ui/open-decisions.md) | Home play panel |
| Minimap, compass, and permissible world information | [ui/open-decisions.md](docs/game/ui/open-decisions.md) | Phase 3 HUD location module |
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
