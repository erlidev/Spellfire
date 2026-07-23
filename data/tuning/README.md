# Tuning tables

Versioned balance data. These files are the only place a balance number is
authored. The Go server embeds them (`data/data.go` → `server/internal/tuning`)
and the Vite client imports the same files (`web/src/tuning.ts`), so both sides
of the prediction loop read one source. Server-only configuration such as the
administrator list is embedded but intentionally not imported by the client.

Rules, from [`invariants.md`](../../docs/game/design/invariants.md) and
[`progression-and-crafting.md`](../../docs/game/design/progression-and-crafting.md#persistence-and-versioning):

- **Content declares its own unlock level.** Every weapon, spell, and gadget row
  carries an `unlock_level`, and `starter: true` marks membership of the basic
  set a new character draws its opening kit from. There is no separate
  level → content mapping to keep in step, and because the unlock ledger stores
  bare IDs, an ID must be unique across those three tables.
- **Saves store references; tables derive values.** A character row holds item
  and unlock IDs, never damage, health, or DPS. Editing a row here therefore
  changes every dependent item with no character migration.
- **Content changes are additive.** Never delete an ID. Retire it by mapping it
  to a replacement or a refund.
- **Damage lives on the band, not the item.** Every damaging row points at a
  `combat.damage_bands` entry. That is what keeps the compressed power band from
  drifting item by item.
- **Crafting changes handling and ceiling, never the damage band.** A component
  declares an open `modifiers` map of multipliers, but only over attributes the
  simulation actually reads: `magazine_size`, `reload_ms`, `cooldown_ms`,
  `windup_ms`, `cost_amount`, `projectile_speed`, `projectile_life`,
  `projectile_radius`, `recoil_degrees`, `spread_degrees`,
  `move_spread_degrees`, and `scope_movement_multiplier`. The last four are
  gunplay and are refused on a blueprint whose weapons have no handling to
  change. Damage is unreachable because it is not a numeric item
  field, and `interval_ms` is rejected outright — fire cadence *is* the DPS axis.
  Multipliers are bounded to `[0.5, 2]`, an exact `1` is rejected as a
  non-change, and every component must declare a material `cost` and a
  plain-language `effect`. A mana crystal may additionally claim `spell_damage`,
  `spell_healing`, `area_radius` — which widens a blast, a field, and the
  telegraph that warns about them together — and `element_damage`, which applies
  only to the `element` the crystal names. A crystal declares that element and
  that modifier together or neither.
- **Every damaging ability declares a dodge vector**, and no projectile may
  have zero travel speed. The loader rejects both, and it also rejects a dodge
  vector the simulation cannot deliver or a cast-time/telegraph claim without
  the windup and shared geometry that make its counterplay real.
- **One ability contract.** Anything that acts — a gun, a spell, a gadget, and
  later a mob — points at an `abilities.json` row for its cost, cadence,
  cooldown, counterplay, delivery, and effects. Weapons, spells, and gadgets
  hold identity only. Delivery is a set of shapes and an ability declares at
  most the ones that fit: a travelling `projectile`, the `blast` its impact
  resolves into, the `deployable` field it leaves standing, the `guard` it
  raises instead of throwing anything, the `wall` it authors, the `blink` that
  moves its caster, the `cleanse` that strips statuses, the `chain` a landed hit
  arcs along, and the `self_effects` it leaves on the caster. `placement` moves
  where all of that lands: a point that far along the committed aim, which is
  also where the telegraph is anchored. An ability that delivers nothing is
  rejected.
- **A deployable is a field, not a wall.** A `deployable` row names an
  `entities.json` archetype with no collision geometry, a radius, and a
  lifetime. Only a field that sets `conceals` takes anything off the wire, and
  only that kind may declare a `reveal_radius` inside which it stops hiding
  anyone; it hides only what its radius covers completely, so the rule matches
  the circle the client draws. A field may also pulse: `tick_ms` paces it,
  `damage_band` and `damage_fraction` price one beat against the shared band,
  `effects` are what each beat applies, and `final_effects` land once on the
  beat that closes the field. `trigger` makes it a trap, sprung and spent by the
  first body to reach it — a trap may not also close with a final pulse. Every
  field changes what happens where a body stands, never where it may walk, and
  must carry a cooldown so one body cannot cover the world in them.
- **A wall is terrain a caster authored.** A `wall` row names a destructible
  archetype that *does* collide, the number of `segments` laid perpendicular to
  the cast, their `spacing`, and how long they stand. It must be placed, must
  hold a cooldown, and may never deal damage: terrain is cover, not a weapon.
- **Bounded steering, not lock-on.** A projectile's `homing` declares a turn
  rate and an acquisition range. The rate is capped at one full turn per second,
  and a homing round may not also land instantly.
- **A guard is spent, not held.** A `guard` row carries `durability` — the
  barrier's own health, which every blocked round is charged to and whose
  overflow reaches the body behind it — plus `regen_per_second` and
  `regen_delay_ms`, which repair it only while it is lowered. A shield drained
  to zero breaks, drops, and returns to service only once it is whole again.
- **A projectile that deals no damage has to deliver something.** A round with
  no damage band must declare a deployable or a blast, or it is a body the world
  carries and nobody ever feels.
- **One telegraph grammar.** Windups emit an authoritative telegraph entity
  whose circle, cone, line, or ring geometry and phase durations come from the
  ability row. Owner type never changes the wire or renderer path.
- **One entity contract.** `entities.json` supplies typed mass, maximum health,
  and local circle/box collision defaults. Runtime instances copy those values
  and may override mutable state or geometry without mutating the table. `-1`
  means immovable mass or undestroyable health.

## Versioning

`manifest.json` carries two numbers:

| Field | Bump when | Consequence |
|---|---|---|
| `version` | Any balance edit | Entitles characters to the global respec/refund |
| `schema_version` | A table changes shape | Requires a matching forward migration and a `tuning.SchemaVersion` bump |

## Files

| File | Contents |
|---|---|
| `manifest.json` | Content and schema versions |
| `admins.json` | Normalized account emails granted administrator authorization |
| `simulation.json` | Tick/send rates, AOI radius, rewind window, interpolation delay |
| `session.json` | Logout linger window and saved-position expiry |
| `entities.json` | Common entity defaults, vision-occlusion/shadow-visibility attributes, spawnability, and generic admin-field/input metadata |
| `world.json` | World radius, spawn radius, chunk size, danger bands, procedural terrain density, fixed fixtures |
| `combat.json` | Role and dodge-vector vocabularies, player movement/resources, universal dash, weight classes, damage bands |
| `loadout.json` | Slot counts per kind and the Mage affinity multiplier |
| `progression.json` | XP curve, the XP each source awards, the starter-kit draw size, the crafted-item capacity, and the developer-mode level-grant bound |
| `elements.json` | The five Mage elements and their roles |
| `abilities.json` | What every action costs, how often it may be used, how it is dodged, what it delivers, and what it applies |
| `effects.json` | Status effects: burn, slow, root, stun, knockback, shield, blind, armor |
| `weapons.json` | Craftable weapons: class, blueprint, magazine, unlock level, weight class, recoil pattern, spread, optional scope, optional material cost, and the ability they fire or the spell they cast |
| `ammunition.json` | Crafted special-ammunition recipes: what they cost, what material they produce, and how many rounds a batch yields |
| `spells.json` | The 5 × 4 spell grid: element, tier, unlock level, and the ability each spell casts |
| `gadgets.json` | Gadgets: the Gunslinger's slot content, its unlock level, and the ability each performs |
| `components.json` | Blueprint slot layouts, and the components that fill them: material cost, behaviour modifiers, and the plain-language effect the crafting UI shows |
| `materials.json` | Material grades, kinds, rows, and the bounded admin grant input |
| `mobs.json` | Mob contracts |
| `biomes.json` | Biomes and the element they align to |
| `outposts.json` | Recall destinations: outpost names and world positions |
| `retired.json` | Withdrawn IDs and the replacement or refund each resolves to |

## Deliberately empty and deliberately absent

Rows are populated only where a design document has settled them.

- `admins.emails` is empty by default. Add account emails to grant the
  server-derived administrator role, then rebuild and restart the server. Email
  matching is case-insensitive; the loader rejects malformed or duplicate
  normalized entries. See [`administration.md`](../../docs/administration.md).
- `entities.*.admin` is the developer catalog and editor contract. A spawnable
  row appears automatically, and each field uses a stable `component.attribute`
  binding with spawn/edit scope plus number, text, select, position, or rotation
  input metadata. Position is a canonical `[x,y]` vector and rotation is
  degrees; bounds remain server-side even though numeric HTML inputs do not
  expose them.
  The explicit server registry resolves those bindings today and can be
  retargeted to ECS component stores later. New UI fields need no client code;
  new runtime attributes need one registry adapter.

- `effects` carries the full Mage-secondary and gadget status catalog. Burn
  ticks and shields resolve against a named band; armor and shields can also
  consume a crafted staff's bounded effective-health multiplier.
- The starter Fire bolt exercises windups and the shared line telegraph. A cast
  pays up front, locks its origin and direction, then delivers only after the
  pending phase; death cancels it into the common resolution flash. The loader
  requires `windup_ms` and `telegraph` together and validates the exact geometry
  each of circle, cone, line, and ring consumes.
- `gadgets` carries the riot shield, smoke canister, and flashbang. The
  remaining gadget slots stay empty, and an empty slot performs nothing rather
  than erroring.
- `progression.sources` prices all four settled XP sources, but only
  `player_kill` has a trigger today. Mob kills (Phase 4.3), harvesting (Phase
  4.1), and outpost discovery (Phase 3) award their row when those systems land;
  the vocabulary is fixed in code, so the table cannot introduce a fifth source
  nothing reads. The curve itself is a placeholder shape — Phase 4.4 tunes it
  against the pacing targets.
- The Gunslinger's opening pool covers pistol, SMG, shotgun, and rifle; the
  Mage draws from the five tier-1 elements. Sustained, burst, and heavy-burst
  bands now separate rate-of-fire identities while resolving to the same Common
  raw-TTK target.
- `materials.materials` carries the structural stock every biome yields and one
  element-aligned shard per biome, because component costs have to name
  something real. Nothing *produces* a material yet — harvest nodes and mob
  drops are Phase 4.1 — so the only source today is the bounded developer-mode
  grant in `materials.admin_grant`.
- `components.components` covers the five-slot gun and two-slot staff
  blueprints. Component tier is funded by matching material grades; the weakest
  selected tier determines item rarity. Prototype Signature parts and the Aegis
  crystal are intentional test content for full-tier rarity and effective-health
  bounds before their eventual acquisition loops exist.
- `mobs.sentry` carries the settled contract — family, silhouette, damage band,
  dodge vector, shared telegraph shape, turret count — and no aggro radius,
  leash, movement, cadence, telegraph timing, or projectile values.
  [`economy-death-and-pve.md`](../../docs/game/design/economy-death-and-pve.md#sentry)
  defers those to implementation and playtesting; writing numbers now would be
  false precision.
- `biomes` carry no placement. Biome geography is Phase 3.
- `outposts` is empty. Outpost geography is Phase 3; the recall search is
  written against the table and resolves to the central hub until it has rows.
- `retired` is empty because nothing has been withdrawn yet. It is the only
  correct way to remove content once a save can name it.

## Retiring content

Never delete an ID from a table. Move it to `retired.json` instead, keyed by the
ID it had, and give it exactly one destination:

```json
{
  "old-iron":   { "kind": "material", "replacement": "iron", "note": "renamed" },
  "lost-alloy": { "kind": "material", "refund": { "iron": 2 }, "note": "recipe withdrawn" }
}
```

`kind` names the table the ID belonged to; retirement never crosses tables.
`replacement` may point at another retired ID, so a chain of renames stays
resolvable. `refund` is the materials owed per unit held, and is the terminal
option when nothing replaces the row. `note` records why.

The loader rejects a retirement that is also a live row, one that declares
neither or both destinations, a refund of an unknown material, and a chain that
dangles or loops. `Tables.Resolve` follows the chain, so the only reference a
caller may drop is an ID no build ever shipped.

## Validation

`server/internal/tuning` re-validates on every load: unknown JSON fields are
rejected, every cross-table reference is resolved, danger bands must run
outward from the hub to the rim with contiguous PvP protection, projectile
kinds must be unique across tables, components must fit a slot their blueprint
exposes and may not modify a magazine on a blueprint whose weapons cast spells,
and each class must have at least one starter weapon — the basic set is a pool a new character draws one of. Unlock IDs must
be unique across the weapon, spell, and gadget tables and must unlock at a level
between 1 and the progression cap, or a ledger entry would be ambiguous or a row
unreachable. The crafted-item capacity must be positive, the progression curve may not shrink between levels, every XP
source the simulation awards must be priced and no other may be declared, and
the starter-kit draw must be wide enough to fill the action bar. The loadout table must lay both classes out over one action bar —
`weapon_slots + gadget_slots` has to equal `spell_slots` — and its affinity
multiplier must leave a tier-4 spell equippable inside those slots, or the
spell grid's own 4 + 2 build would be unbuildable. Abilities add their own: the cost kind must be one the simulation can
charge, a magazine weapon's ability must spend ammunition it holds, every
applied effect must exist, an effect must be of a kind the world can run and may
only carry the fields its kind uses, and a damaging ability must declare a band
and an honoured dodge vector. Telegraph rows must pair with a positive windup,
carry positive active/resolved phase times, and use only their shape's geometry;
mob telegraph shapes come from the same vocabulary. Gunplay adds its own: every
gun needs a weight class, a recoil pattern that both walks and recovers, and a
moving spread wider than its standing one; a weight class may only slow its
carrier; a scope must cost movement, steady the shot, and see further; an
instantly landing round must require a scope, claim `scoped_commit`, and have
travelling range past its cap; a guard must cover less than a full circle, cost
mobility, deal no damage, and carry durability that recovers — a barrier with no
durability is invulnerability with an arc drawn on it; a withheld category must cost materials and may
not be in a basic set; and crafted ammunition must be produced by a recipe,
cost something, and never cost the material it produces. A deployable must name
a live archetype with no collision geometry, cover ground, expire, reveal inside
a gap smaller than itself, be delivered by something that travels, deal no
damage, and hold a cooldown. Failures list every problem at once — run
`go test ./server/...`.
