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
- [x] Eight-direction normalised movement, circular world bound, static tree collision
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

Component, material, and biome-placement rows are intentionally empty until the phases that consume them; the Sentry row carries its settled contract without the values [economy-death-and-pve.md](docs/game/design/economy-death-and-pve.md#sentry) defers to implementation. Runtime table delivery to a live client stays in Phase 8.

### 1.2 Persistence & migration
- [x] Read `schema_version` and run sequential forward migrations — `PRAGMA user_version` for the database schema, `characters.schema_version` for the record shape ([sqlite.go](server/internal/store/sqlite.go), [model.go](server/internal/model/model.go), [architecture.md](docs/architecture.md#persistence-and-migration))
- [x] Retired-ID → replacement/refund resolution map; never delete an ID ([retired.json](data/tuning/retired.json), `Tables.Resolve` in [tuning.go](server/internal/tuning/tuning.go), [invariants.md](docs/game/design/invariants.md))
- [x] Persist character position, carried inventory, and unlocked outposts; saved every 15 s, at logout, and at shutdown, restored and re-validated on join ([engine.go](server/internal/game/engine.go), [world.go](server/internal/game/world.go))
- [x] Store crafted items as recipe + component IDs, never a stat snapshot (`crafted_items`; `model.CraftedItem`)
- [x] 10 s logout linger: the body stays in the world, killable and unable to act, so disconnecting is not an escape ([session.json](data/tuning/session.json), [engine.go](server/internal/game/engine.go))
- [x] Saved position expires after 30 min offline and recalls to the nearest unlocked outpost or the hub ([outposts.json](data/tuning/outposts.json), `World.recallDestination`)
- [x] One body per account: a second character's join is refused while the first is in the world, lingering included, so switching characters is not a combat-log escape (`Engine.Join`/`ErrAccountInWorld`, [architecture.md](docs/architecture.md#one-body-per-account))

Carried materials and unlocked outposts round-trip through the world but nothing mutates them yet: harvesting is Phase 4.1, outpost discovery is Phase 3, and crafting is Phase 2.3. `outposts.json` ships empty, so every recall resolves to the hub until Phase 3 places them. A lingering body is not flagged on the wire, so it reads as a motionless player rather than one visibly logging out — the field belongs with the Phase 1.5 per-entity state expansion.

### 1.3 Server-side ability/effect framework
- [ ] One authoritative ability system replacing the ad-hoc branches in `stepPlayer`/`tryFire` ([world.go:161-233](server/internal/game/world.go#L161-L233))
- [ ] Ability schema declares cost, cooldown, telegraph, and **a mandatory dodge vector** ([invariants.md](docs/game/design/invariants.md))
- [ ] Status-effect layer: burn/DoT, slow, root, stun, knockback, shields
- [ ] Reject at load any damaging ability with no declared dodge vector

### 1.4 Damage attribution
- [ ] Per-target contribution ledger in the projectile resolver ([world.go:299-306](server/internal/game/world.go#L299-L306))
- [ ] Kill credit by most damage dealt, not last hit ([squads-and-world-bosses.md](docs/game/design/squads-and-world-bosses.md#squads))
- [ ] Combat log surface reusable by drop ownership and boss ranking

### 1.5 Protocol expansion
- [ ] Extend `Entity.Type` beyond `PLAYER`/`PROJECTILE` ([game.proto:37](proto/game.proto#L37)): mob, drop, node, telegraph, deployable, boss
- [ ] Per-entity fields for element, allegiance/squad, telegraph state, invulnerability
- [ ] Add an interact button to the input bitfield ([game.proto:18](proto/game.proto#L18))
- [ ] Measure snapshot size before and after; record the bandwidth budget in `docs/architecture.md`

### 1.6 Telegraph grammar (built here, not in Phase 6)
- [ ] Shared data-driven renderer for translucent circle / cone / line / ring ([visual-direction.md](docs/game/design/visual-direction.md#readability-system))
- [ ] Opacity encodes pending → active → resolved; resolution flash
- [ ] Both the spell system and the Sentry consume it; nothing hand-rolls a telegraph

---

## Phase 2 — Content axis

### 2.1 Equipped slots & loadout
- [ ] Slot model: one weapon, gadget slots, six spell slots ([progression-and-crafting.md](docs/game/design/progression-and-crafting.md#progression-layers))
- [ ] Server-side loadout validator: slot limits, Mage affinity (tier-N needs N−1 same-element spells)
- [ ] Loadout lock outside safe zones, enforced server-side on mutation — the keystone economy rule
- [ ] Free respec inside safety; global respec/refund on a balance patch
- [ ] Menu Loadout section: view anywhere, edit only in safety, with an explicit lock reason ([system-interfaces.md](docs/game/ui/system-interfaces.md#safe-zone-loadout-and-crafting))

### 2.2 Unlock ledger & starter kit
- [ ] Flat permanent unlock ledger for gun parts, spells, and keystone IDs ([model.go:18-25](server/internal/model/model.go#L18-L25) holds only level/XP)
- [ ] XP sources and a level → unlock mapping; level currently drives nothing
- [ ] Starter kit on creation: one random basic class weapon + a random set of low-tier unlocks ([progression-and-crafting.md](docs/game/design/progression-and-crafting.md#starter-kit))
- [ ] Test: a zero-material character can fill a coherent loadout immediately

### 2.3 Slotted-blueprint crafting
- [ ] Blueprint + slot + component definitions with material costs and **behavioural** (not power) effects ([progression-and-crafting.md](docs/game/design/progression-and-crafting.md#slotted-blueprint-crafting))
- [ ] Gun slots: muzzle, barrel, scope, trigger, magazine
- [ ] Staff slots: core, focus, conduit
- [ ] Crafting gated to safe zones; raw materials must be hauled there
- [ ] Crafting UI: blueprint, slots, compatible components, owned/required materials with shortfalls, plain-language behaviour changes, spend confirmation, rejection outcomes ([system-interfaces.md](docs/game/ui/system-interfaces.md#safe-zone-loadout-and-crafting))

### 2.4 Gunslinger kit
- [ ] Recoil model; every shot currently leaves on the exact aim vector ([world.go:249](server/internal/game/world.go#L249))
- [ ] Move-spread: firing while moving is currently identical to standing ([gunslinger.md](docs/game/design/gunslinger.md#gunplay))
- [ ] Weight classes with recoil / spread / slowdown tradeoffs inside one damage band
- [ ] Multiple guns and gun categories; magazine size becomes a weapon property (hardcoded `10` at [world.go:114](server/internal/game/world.go#L114), [:140](server/internal/game/world.go#L140), [:199](server/internal/game/world.go#L199))
- [ ] Crafted special ammunition (rockets) as a finite, craftable resource
- [ ] Snipers: hitscan to a weapon cap, then travel-time projectile with falloff and hard max range ([gunslinger.md](docs/game/design/gunslinger.md#snipers))
- [ ] Scope mode: peripheral blackout, controllable near-area view, server-side scoped state affecting movement and spread; camera exception in [game-view-and-hud.md](docs/game/ui/game-view-and-hud.md#camera)
- [ ] Riot shield: frontal arc only, slows the user, locks fire, blocks bullets/projectiles, does not block ground AoE ([gunslinger.md](docs/game/design/gunslinger.md#defense)) — needs arc blocking in the resolver ([world.go:280-310](server/internal/game/world.go#L280-L310))
- [ ] Smoke (LOS break) and flashbang (aim disruption) deployables — depends on 2.6
- [ ] Rare materials gate heavy weapons economically, never statistically

### 2.5 Mage kit
- [ ] Per-spell cooldowns — the missing second resource axis alongside mana ([mage.md](docs/game/design/mage.md#mana-and-cooldowns)); cost is a hardcoded `12` ([world.go:227](server/internal/game/world.go#L227))
- [ ] Five elements as data and behaviour: Fire, Frost, Storm, Arcane, Earth ([mage.md](docs/game/design/mage.md#elements))
- [ ] Element secondaries: burn/DoT, light mitigation, blink-on-hit, shields/dispel/teleport, walls/knockback/armor
- [ ] Spell tiers 1–4 scaling mana, cooldown, telegraph, payoff, and whiff punishment
- [ ] Staff components alter cast speed, mana cost, projectile/area shape, and element bias without touching the damage band
- [ ] Test: no spell delivers instant point-and-click damage

### 2.6 Line of sight
- [ ] Vision/targeting occlusion; trees currently block projectiles only
- [ ] Substrate for smoke and for the Mage/Gunslinger LOS matchup ([combat.md](docs/game/design/combat.md#time-to-kill))

### 2.7 Roles, keystones, and band enforcement
- [ ] Cover the seven combat roles across both classes; only Damage exists ([combat.md](docs/game/design/combat.md#shared-combat-roles))
- [ ] Keystones: behaviour-changing tradeoffs (empowered-but-costlier casts, overheat-instead-of-reload)
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
- [ ] Carried-material inventory on the player, distinct from crafted items
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

- [ ] Actor plates: display name, health, and squad/threat marker with density-based simplification and the specified occlusion priority ([game-view-and-hud.md](docs/game/ui/game-view-and-hud.md#actor-labels))
- [ ] Enemy Mage mana stays private
- [ ] Lower-centre ability bar: slots, cooldowns, charges, class resource, bindings, universal dash
- [ ] Conditional collapsing HUD modules that never hide local health, critical resources, or menu access
- [ ] Contextual prompts in one consistent interaction area covering harvest, loot priority, safe-zone transitions, and actionable failures
- [ ] Boundary-crossing announcements that teach once and then condense to persistent state
- [ ] Menu sections still missing: Crafting, World, Squad, Activity ([game-menu.md](docs/game/ui/game-menu.md#information-architecture))
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

---

## Phase 8 — Scale & operations

- [ ] Spatial hash or quadtree replacing the O(players + projectiles + colliders) per-client snapshot path ([snapshot.go](server/internal/game/snapshot.go))
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
