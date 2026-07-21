# SpellFire — Implementation Gap Ledger

Everything the game design ([`docs/game/design/`](docs/game/design/README.md)) specifies that is
**absent** or **only partially present** in the code as of v0.1. Section numbers below predate the
split of the single GDD into topic files; each section links to its current owner. This is a gap list,
not a schedule; §14 proposes phasing.

Legend:

| Mark | Meaning |
|---|---|
| ✗ | Not implemented at all |
| ◐ | Partially implemented — the shape exists but the GDD rule is not satisfied |
| ⚠ | Blocked on an **OPEN** GDD decision (design must be resolved before or alongside implementation) |
| ✓ | Fully implemented |

Baseline for comparison — what *is* implemented: accounts/sessions/characters (SQLite), one shared
60 Hz authoritative world, 8-direction movement, universal dash, aim, one starter attack per class,
mana regen, magazine/reload, projectile damage, death, hub respawn, static trees, hub/fringe PvP
protection, AOI culling, server rewind, client prediction + reconciliation, entity interpolation, and
a procedural Pixi renderer (grid, bodies, held weapon, health bar, name).

---

## 1. Combat model (GDD §2)

- ◐ **Raw 3s TTK band exists but only for one weapon each.** `game.Tuning` hardcodes a single
  100 HP / 10 dmg / 300 ms cadence pair ([world.go:35-43](server/internal/game/world.go#L35-L43)).
  There is no per-weapon/per-spell stat resolution, so the "compressed band" is a constant, not an
  enforced invariant across a catalogue. Needs a damage/DPS resolver that computes from item data and
  a test asserting every weapon lands inside the band (P2, §11).
- ✗ **Effective-vs-raw TTK tools.** No mitigation, no shields, no escape beyond the base dash, no
  line-of-sight breaking. Currently every fight *is* raw TTK, which the GDD explicitly calls
  non-functional for a build with no defensive answer (§2.1).
- ✗ **The seven combat roles (§2.2).** Only Damage exists. No Burst, Control, Mobility (beyond the
  floor dash), Sustain, Zone, or Range differentiation on either class.
- ✗ **Equipped-slot budget.** No slots at all — no weapon slot, no gadget slots, no 6 spell slots. The
  "budget enforcement is by equipped slots" rule has nothing to enforce.
- ✗ **Keystones (§2.3).** No keystone concept anywhere: not in the model, the store, the protocol, or
  the client.
- ◐ **Universal dash (§2.4).** Implemented correctly as a floor: fixed distance, 2.2 s cooldown, no
  i-frames ([world.go:187-194](server/internal/game/world.go#L187-L194)). Missing: it is currently the
  *only* mobility in the game; Storm/movement spells and mobility gadgets that stack on top do not exist.
- ✗ **Line of sight.** Trees block projectiles but nothing occludes vision or targeting, so smoke and
  the LOS half of the mage/gunslinger matchup have no substrate.

## 2. Class structure (GDD §3)

- ◐ **Two classes are distinct in feel only cosmetically.** Class currently changes body shape,
  projectile size/speed, and resource type (mana vs magazine). None of the axes in the §3 table —
  cover-shooter vs telegraph-caster model, aim tax asymmetry, specialisation axis, build visibility —
  are expressed mechanically.
- ✗ **Build-visibility asymmetry (§3, §10.5).** The gunslinger's held gun is a fixed silhouette that
  encodes nothing (no weight class, range, or rate-of-fire read). The mage's element is not readable
  because elements do not exist. The "toolkit hidden until used" rule has no toolkit.

## 3. Gunslinger (GDD §4)

- ✗ **Recoil.** No recoil model at all; every shot leaves the muzzle on the exact aim vector
  ([world.go:249](server/internal/game/world.go#L249)).
- ✗ **Move-spread.** Firing while moving is identical to firing while standing — the GDD's "constant
  micro-decision" (§4.1) does not exist.
- ✗ **Weight classes.** One gun, no weight, no movement penalty, no handling differentiation.
- ✗ **Multiple guns / gun categories.** A single hardcoded bullet.
- ✗ **Special ammo (RPG-class weapons whose ammo is crafted).** Ammo is infinite-with-reload only;
  magazine size is the literal `10` in three places
  ([world.go:114](server/internal/game/world.go#L114), [:140](server/internal/game/world.go#L140),
  [:199](server/internal/game/world.go#L199)) rather than a weapon property.
- ✗ **Snipers and the hitscan exception (§4.2).** No hitscan-to-range-cap-then-projectile weapon, no
  damage falloff, no hard max range.
- ✗ **Scoping.** No scope mode, no screen blackout with a manipulable scope view, no peripheral-awareness
  vulnerability window. This also needs a client camera/render mode and a server-side awareness of
  scoped state (movement penalty, spread change).
- ✗ **Deployables — smoke, flashbang (§4.3).** Requires LOS occlusion and a client-side aim-disruption
  effect, neither of which exist.
- ✗ **Riot shield (§4.3).** The required anti-burst hard stop. Must be directional (frontal arc only),
  slow the user, lock their fire, block bullets/projectiles from the front, and *not* block ground AoE.
  Nothing in the projectile resolver ([world.go:280-310](server/internal/game/world.go#L280-L310))
  supports arc-based blocking.
- ✗ **Unlock ledger of gun parts and blueprints (§4.4).** `model.Character` holds only `Level`/`XP`
  ([model.go:18-25](server/internal/model/model.go#L18-L25)). No unlocks table, no blueprint IDs.
- ✗ **Material-gated heavy weapons.** No materials, no gate.
- ✗ **Starter kit.** Now specified: one random basic class weapon plus a small random set of low-tier
  unlocks ([progression-and-crafting.md](docs/game/design/progression-and-crafting.md#starter-kit)).
  The code hardcodes a single implicit kit and has no unlock ledger or randomisation to draw from.

## 4. Mage (GDD §5)

- ✗ **The "no instant point-and-click damage" rule has nothing to enforce.** The one mage attack is a
  travel-time projectile, which satisfies the rule by accident. There is no spell system where the rule
  could be violated or validated.
- ◐ **Resource model (§5.1).** Mana pool + regen implemented
  ([world.go:195-197](server/internal/game/world.go#L195-L197)); cost is a hardcoded `12`
  ([world.go:227-229](server/internal/game/world.go#L227-L229)). **Missing the entire second axis:**
  per-spell individual cooldowns. Without cooldowns there is no high-tier/low-tier split and no
  cognitive resource layer.
- ✗ **Five elements (§5.2).** Fire, Frost, Storm, Arcane, Earth do not exist as data or behaviour. The
  mage's projectile is called `"fireball"` but carries no element semantics.
- ✗ **Element secondary tools** — burn/DoT, light mitigation, blink-on-hit, shields/dispel/teleport,
  walls/knockback/armor. No status-effect system, no shield system, no knockback, no wall/terrain
  spawning at all.
- ✗ **Spell tiers 1–4 and tier semantics (§5.3).** No tiers, no cooldown/mana/telegraph scaling.
- ✗ **Element affinity loadout rule.** "Tier-N requires N−1 same-element spells in the 6 slots" —
  needs slots, elements, tiers, and a server-side loadout validator. None exist.
- ✗ **Staffs (§5.4).** The rendered staff is a fixed decorative shape
  ([view.ts:122](web/src/game/view.ts#L122)) with no components, no core/focus/conduit slots, and no
  effect on spell behaviour.
- ✓ **Ranged-poke role gap — resolved as intentional absence.** The five-element table is final; a poke
  tool, if ever needed, becomes a secondary on an existing element, not a sixth school. No longer
  blocks §5.2.

## 5. Progression & crafting (GDD §6)

- ✗ **Character layer — unlock ledger.** `Level`/`XP` are persisted but never awarded, never read by
  the simulation, and drive nothing. There is no XP source (no kills, no mobs, no harvest) and no
  level-to-stat-band mapping.
- ✗ **Crafting layer.** No items table, no recipes, no crafted-item persistence.
- ✗ **Loadout layer.** No equipped set exists, therefore no loadout at all.
- ✗ **Loadout-lock outside safe zones — "the keystone rule of the whole economy" (§6.1).** Nothing to
  lock, and no server-side safe-zone gate on mutation.
- ✗ **Slotted-blueprint crafting, one system skinned twice (§6.2).** Blueprint definitions, slot
  definitions, component options with material costs, and behavioural (not power) effects — none exist.
  This is the single largest missing subsystem and both classes depend on it.
- ✗ **Crafting is safe-zone-only, materials must be hauled.** No crafting UI, no station, no gate.
- ✗ **Free respec + global respec-on-balance-patch.** No build to respec.
- ◐ **Persistence & versioning (§6.3).** The *principle* is honoured — `characters` stores
  `level, xp, schema_version` and no computed stats
  ([sqlite.go:45-50](server/internal/store/sqlite.go#L45-L50)). But:
  - ✗ **Versioned tuning tables.** Balance lives in a Go struct literal
    ([world.go:35-43](server/internal/game/world.go#L35-L43)), not in versioned, data-driven tables.
    "Nerfing a barrel edits one row" is not currently possible.
  - ✗ **Forward migration runner.** `schema_version` is written but never read; no sequential migration
    mechanism exists.
  - ✗ **Deprecate-never-delete ID resolution.** No retired-ID → replacement/refund map.
  - ✗ **Recipe-shaped item storage.** No items are stored at all, so "stored as part IDs, never a stat
    snapshot" is untested in practice.

## 6. World structure (GDD §7)

- ◐ **Radial danger gradient (§7.1).** Only two of four bands exist as geometry: a safe hub
  (`SafeRadius` 430) and a PvP-protected fringe (`PvPRadius` 1000), out to a 3000-unit world edge.
  **Missing:** the Frontier (T2) / Deadlands (T3) distinction as a first-class concept, any
  danger-tier lookup used by loot, mobs, insurance, or ambient rendering.
- ✗ **Material grade by radius, and the convex reward curve.** No materials, no reward scaling, so the
  entire "bracket separation is economic, not walled" mechanism is absent.
- ✗ **Biomes (§7.2).** No biome axis at all. Needs: a biome map (type) crossed with radius (grade), a
  biome lookup by position, biome-themed props/nodes, and biome-tinted ambient.
- ✗ **Universal structural mats available everywhere / element mats biome-gated.** No mats.
- ✗ **Outposts (§7.3).** Only one safe zone exists (the world-centre hub). No multiple outposts, no
  discover-to-unlock, no per-character unlocked-outpost list, no spawn selection.
- ✗ **Travel & spawn choice.** Respawn is hardcoded to the origin
  ([world.go:134-144](server/internal/game/world.go#L134-L144)).
- ✗ **Mounts/vehicles**, and the "no fast-travel while carrying raw materials" rule.
- ✗ **Outpost safety rules.** Now specified
  ([world.md](docs/game/design/world.md#outpost-safety)): a no-PvP radius around every outpost plus
  brief exit invulnerability that breaks on the player's own hostile action. The hub/fringe PvP
  protection is radius-from-origin only ([world.go](server/internal/game/world.go)); there is no
  per-outpost zone, no invulnerability state on `Player`, and no protocol field to render either.
  Outposts are also fixed and unaffectable, so no capture/damage surface is needed.

## 7. Economy, death & PvP (GDD §8)

- ✗ **Haul-to-craft core loop (§8.1).** The whole loop — carry unspent mats through danger, bank by
  crafting — does not exist.
- ✗ **Carried-material inventory.** No inventory on `Player` or `Character`.
- ✗ **Death penalty (§8.2).** Death currently costs nothing but position: no material loss, no drops.
- ✗ **Tier-scaled insurance.** Requires danger-tier lookup + inventory.
- ◐ **Respawn.** Instant, free, at origin. Now specified
  ([economy-death-and-pve.md](docs/game/design/economy-death-and-pve.md#respawn)): a ~5 s timer and a
  free choice among every discovered outpost plus the hub, defaulting to the nearest, with no respawn
  cost. Missing: the timer, per-character outpost discovery, the nearest-outpost default, a scannable
  destination list with distance and danger band, and timer-expiry fallback to the default. Rim deaths
  need no special case — the Deadlands has no outposts.
- ✗ **Dropped materials (§8.3):** ground drops, 30 s killer-squad exclusivity, free-for-all after,
  15 min despawn, and pickup-is-free/crafting-is-gated. Needs a world-entity type for drops plus
  ownership/timer state, and a protocol entity type to render them.
- ✗ **Harvest nodes (§8.4).** No nodes, no interact button, no channel-and-interrupt harvest, no
  grade-scaled harvest time. Requires a new input button (the `buttons` bitfield in
  [game.proto:18](proto/game.proto#L18) has room) and an interact/channel state machine.
- ✗ **Mobs of any kind (§8.4, §8.5).** The world has no NPCs — `World` holds only players, projectiles,
  and static colliders ([world.go:89-97](server/internal/game/world.go#L89-L97)).
- ✗ **The Sentry mob class (§8.5)** specifically: fused body-plus-rotating-turret entity, patrol,
  aggro-nearest-in-radius, drive-and-orbit chase, independent turret tracking, leash + disengage +
  return, telegraphed dodgeable projectile shots, per-tier scaling (speed / cadence / telegraph speed /
  turret count), chokepoint and node-cluster placement, biome tint + hostility ring. Also needs:
  a `MOB` entity type in the protocol, an AI tick in the world step, and a mob-turret render path
  distinct from the player held-weapon path.
- ✗ **Sentry values — deliberately unspecified.** Aggro radius, leash range, reset rule, move/turn
  speed, cadence, and turret count carry no design numbers and will not get them before a Sentry
  exists in the build; they are set at implementation and fixed by playtesting, per variant. Sentry is
  a family, so the AI and tuning must be data-driven per variant from the start rather than one
  hardcoded mob. This is no longer a design blocker.
- ✗ **Kill credit by most-damage-dealt (§9.1).** No damage attribution is recorded; the projectile
  resolver applies damage without tracking a per-target contribution ledger
  ([world.go:299-306](server/internal/game/world.go#L299-L306)).

## 8. Squads & world bosses (GDD §9)

- ✗ **Squads entirely.** No party formation, no 4-cap, no squad ID on players, no squad-aware
  rendering (outline/ring allegiance currently only distinguishes self vs. hostile,
  [view.ts:120-124](web/src/game/view.ts#L120-L124)).
- ✗ **Squad loot culture** (safe-zone-set free-for-all vs. auto-split-among-nearby).
- ✗ **Squad-exclusive 30 s drop window** (depends on squads + drops).
- ✗ **World bosses (§9.2):** biome-themed large procedural constructs, region-scaled difficulty,
  squad-pooled contribution ranking, top-4 rare loot split by contribution ratio, and a
  percent-of-boss-HP participation floor for common loot.

## 9. Visual & art direction (GDD §10)

- ◐ **Primitive vocabulary + outline rule (§10.2).** Honoured — everything is `Graphics` primitives
  with flat fills and dark outlines, and there are no bitmaps in the play space. Keep this invariant
  under review as content is added.
- ◐ **The grid (§10.2).** Present but a single fixed colour and 80-unit cell
  ([view.ts:86-94](web/src/game/view.ts#L86-L94)); not palette-driven.
- ✗ **Palette system — global base, regional shift (§10.4).** All three ambient channels are static:
  background value/tint, grid colour/opacity, and ambient saturation do not shift by biome or darken /
  desaturate toward the rim. This is the mechanism that makes danger depth readable at a glance.
- ✗ **Element reserved hues.** No element hue vocabulary; colours are ad-hoc literals
  ([view.ts:10-13](web/src/game/view.ts#L10-L13)). Needs a shared, single-source palette module
  (ideally derived from the same tuning data the server uses).
- ✗ **Procedural weapons from recipe (§10.5).** Gun and staff silhouettes are hardcoded shapes
  ([view.ts:114-126](web/src/game/view.ts#L114-L126)), not a function of slotted parts. "The crafting
  data and the rendered shape are the same data" is the intent and it is unimplemented.
- ✗ **Mage body tinted by dominant element.** Body is a fixed orange.
- ◐ **Channel separation (§10.6).** Fill=identity / outline=allegiance is partially honoured (self vs.
  hostile outline). **Missing:** squad and neutral allegiance states, and the **overhead threat/squad
  ring** — which is the *required redundant non-hue cue* for colourblind safety.
- ✗ **Telegraph grammar (§10.6).** No telegraphs of any kind: no translucent ground shapes (circle,
  cone, line, ring), no fill-over-pre-resolve-window, no resolution flash. Both the mage spell system
  and the Sentry depend on this; build it once as a shared, data-driven renderer.
- ✗ **Projectile shape encodes type.** Two hardcoded shapes exist; there is no type→silhouette table
  covering bullets, sniper rounds, spell bolts, AoE seeds, and thrown gadgets.
- ✗ **Feedback forms (§10.6):** damage flash on the hit body, mana bar (only health is drawn —
  [view.ts:109](web/src/game/view.ts#L109); mana is HUD-only), and the procedural death burst
  (death currently just fades the actor to 32% alpha, [view.ts:106](web/src/game/view.ts#L106)).
- ✗ **Colourblind redundancy audit.** No hue-coded distinction currently carries a guaranteed
  non-hue cue; needs to be a checklist enforced per new visual, not retrofitted.
- ◐ **Safe vs. open world visually distinct (§10.7).** Only a single ring stroke marks the hub
  ([view.ts:92](web/src/game/view.ts#L92)); ambient does not change, so the loadout-lock boundary is not *seen*.
- ✗ **Biome scatter props.**
- ✗ **Effects and juice (§10.8):** muzzle flashes, impact rings, explosion bursts, recoil/scale pops,
  camera feedback, sparing glow. Currently there is no effect layer at all — a significant part of the
  perceived quality budget.
- ◐ **Hitbox honesty (§10.8).** Player collision is a radius-20 circle
  ([world.go:38](server/internal/game/world.go#L38)) while the gunslinger is drawn as a ~35×33 quad
  ([view.ts:124](web/src/game/view.ts#L124)) — the drawn shape and the hitbox do not match. Either
  reconcile the shapes or render an explicit hitbox-matching silhouette.
- ⚠ **Palette validation** (still OPEN) — exact element hues and a colourblind-safe pass. Deferred
  deliberately: the art style may still shift, so hues get validated against real fights rather than
  chosen up front. Build the palette module with swappable values so a later revision is a data
  change, and enforce the redundant non-hue cue regardless of which hues win.

## 10. Cross-cutting invariants (GDD §11)

Each of these is an invariant that currently has no enforcement mechanism. They should become tests or
lint-style checks as the systems they govern land, not prose:

- ✗ "Stronger = higher skill ceiling at the same power band" — needs an automated band check across the
  weapon/spell/component catalogue.
- ✗ "Access is gated; power is not" — needs unlock gating to exist first.
- ✗ "Specific mats gated, common mats universal" — needs the material taxonomy.
- ✗ "Breadth is a safe-zone advantage only" — needs the loadout lock.
- ◐ "Every damaging tool has a dodge vector and a punish window" — true today only because both
  attacks are projectiles; no rule prevents adding an instant-damage tool. Encode it in the ability
  schema (every damaging ability must declare a dodge vector).
- ◐ "Store references, derive from versioned tables" — half done (see §5 above).
- ✗ "Never retroactively confiscate" — needs the grandfathering policy in the migration runner.
- ✗ "Incentives reward cooperation; nothing taxes it" — needs squads and rewards.
- ◐ "Play space is 100% procedural" — currently true; keep enforced.
- ✗ "Form encodes function" — mostly unrealised (see §9 above).
- ✗ "Every hue-coded distinction carries a redundant non-hue cue."

## 11. OPEN design decisions to resolve

One design decision still blocks code:

| Area | Blocks |
|---|---|
| Palette validation (hues + colourblind pass) | The palette module (§10.4) — build it swappable and ship without final hues |

Resolved, now implementation work tracked in the sections above:

| Area | Resolution |
|---|---|
| Ranged-poke role | Intentional absence; five elements are final |
| Outpost blockading | No-PvP radius per outpost + brief exit invulnerability; outposts are unaffectable |
| Respawn | ~5 s timer; free choice of any discovered outpost or the hub, nearest preselected; no respawn cost |
| Mob aggro / leash / kite | Per-mob-class contract, not a shared template |
| Sentry values | Deferred to implementation and playtesting, per variant |
| Starter kit | Random basic class weapon + random low-tier unlocks |
| Progression pacing | ~1 h to a coherent build, ~10 h to rim viability, mastery beyond |
| Renderer | Pixi.js, kept behind `view.ts` |

## 12. Deferred by choice (GDD §12.3) — not gaps, but tracked

Netcode/server architecture at scale, monetisation/cosmetics, marketplace & trading, social/guilds/
territory, onboarding & tutorial, mob classes beyond the Sentry, and the full numeric balance pass.
Listed so they are not mistaken for oversights.

## 13. Engineering prerequisites the GDD implies

Not GDD sections, but the GDD's targets cannot be met without these:

- ✗ **Data-driven content pipeline.** Weapons, spells, components, materials, mobs, and biomes should
  load from versioned tuning tables shared by server and client (§6.3, §10.4), not Go/TS literals.
  Nearly every item above depends on this existing first.
- ✗ **Protocol expansion.** `Entity.Type` currently has only `PLAYER` and `PROJECTILE`
  ([game.proto:37](proto/game.proto#L37)). Mobs, drops, nodes, telegraphs, deployables, and bosses each
  need representation, plus per-entity fields for element, allegiance/squad, and telegraph state.
  Snapshot size should be measured before adding them.
- ✗ **World-state persistence.** Character position, inventory, and unlocked outposts are not stored;
  a disconnect resets you to the hub. `store.Store` has no surface for any of it.
- ✗ **Spatial index.** Snapshot building is O(players + projectiles + colliders) per client with a
  distance check; the GDD's 100+ concurrent target needs a spatial hash/quadtree, snapshot deltas, and
  a bandwidth budget (already flagged in `docs/architecture.md`).
- ✗ **Server-side ability framework.** Every remaining combat feature (spells, gadgets, deployables,
  telegraphs, status effects, shields, knockback) needs one authoritative ability/effect system rather
  than more ad-hoc branches in `stepPlayer`/`tryFire`. Build this before the second weapon or spell.
- ✗ **Combat log / damage attribution.** Prerequisite for kill credit, boss ranking, and drop ownership.

## 14. Suggested phasing

Dependency-ordered; each phase unblocks the next.

1. **Foundations** — versioned tuning tables + data-driven content loading; ability/effect framework;
   damage attribution; protocol entity-type expansion; inventory & position persistence.
2. **Content axis** — multiple guns and gun parts, spells with elements/tiers/cooldowns, slotted
   blueprint crafting, unlock ledger, loadout + safe-zone lock. Add the band-invariant tests here.
3. **World axis** — danger tiers T1–T3, biomes, materials (type × grade), harvest nodes, outposts and
   discover-to-unlock, ambient palette shift.
4. **Loop axis** — Sentries, drops and death penalty with insurance, haul-to-craft, XP and levelling.
5. **Social axis** — squads, squad loot rules, world bosses.
6. **Presentation pass** — telegraph grammar (needed earlier if spells land first), procedural weapons
   from recipe, effects/juice, overhead rings, colourblind audit, hitbox reconciliation.

Note: the telegraph grammar (§10.6) is a phase-6 *item* but a phase-2 *dependency* — spells and
Sentries are both unreadable without it. Build it with the ability framework.
