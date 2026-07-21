# Tuning tables

Versioned balance data. These files are the only place a balance number is
authored. The Go server embeds them (`data/data.go` → `server/internal/tuning`)
and the Vite client imports the same files (`web/src/tuning.ts`), so both sides
of the prediction loop read one source. Server-only configuration such as the
administrator list is embedded but intentionally not imported by the client.

Rules, from [`invariants.md`](../../docs/game/design/invariants.md) and
[`progression-and-crafting.md`](../../docs/game/design/progression-and-crafting.md#persistence-and-versioning):

- **Saves store references; tables derive values.** A character row holds item
  and unlock IDs, never damage, health, or DPS. Editing a row here therefore
  changes every dependent item with no character migration.
- **Content changes are additive.** Never delete an ID. Retire it by mapping it
  to a replacement or a refund.
- **Damage lives on the band, not the item.** Every damaging row points at a
  `combat.damage_bands` entry. That is what keeps the compressed power band from
  drifting item by item.
- **Every damaging ability declares a dodge vector**, and no projectile may
  have zero travel speed. The loader rejects both, and it also rejects a dodge
  vector the simulation cannot deliver or a cast-time/telegraph claim without
  the windup and shared geometry that make its counterplay real.
- **One ability contract.** Anything that acts — a gun, a spell, and later a
  mob or a deployable — points at an `abilities.json` row for its cost,
  cadence, cooldown, counterplay, delivery, and effects. Weapons and spells hold
  identity only.
- **One telegraph grammar.** Windups emit an authoritative telegraph entity
  whose circle, cone, line, or ring geometry and phase durations come from the
  ability row. Owner type never changes the wire or renderer path.

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
| `admin_tools.json` | Developer-mode spawn catalog, editable per-entity fields, and bounded player overrides |
| `simulation.json` | Tick/send rates, AOI radius, rewind window, interpolation delay |
| `session.json` | Logout linger window and saved-position expiry |
| `world.json` | World radius, spawn radius, danger bands, procedural tree parameters |
| `combat.json` | Role and dodge-vector vocabularies, player body, universal dash, damage bands |
| `elements.json` | The five Mage elements and their roles |
| `abilities.json` | What every action costs, how often it may be used, how it is dodged, what it delivers, and what it applies |
| `effects.json` | Status effects: burn, slow, root, stun, knockback, shield |
| `weapons.json` | Craftable weapons: class, blueprint, magazine, and the ability they fire or the spell they cast |
| `spells.json` | Spells: element, tier, and the ability they cast |
| `components.json` | Blueprint slot layouts and the components that fill them |
| `materials.json` | Material grades, kinds, and rows |
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
- `admin_tools.json` contains every entity family the live world can currently
  materialize: temporary player fixtures, projectiles, and telegraphs. Rows are
  rendered as a searchable admin catalog and validated again by the server;
  adding another row of one of those kinds requires no UI code. A new entity
  kind still needs its authoritative world executor before it can be added.

- `effects` is empty. The simulation runs all six kinds — burn ticks from a
  band, slows scale movement and take the strongest rather than compounding,
  roots stop movement, stuns stop everything, knockbacks override input and
  cancel a dash, shields absorb before health — but no design document has
  settled a magnitude. Phase 2.4's gadgets and Phase 2.5's element secondaries
  author the rows; the tests exercise the layer against rows they add
  themselves.
- The starter Fire bolt exercises windups and the shared line telegraph. A cast
  pays up front, locks its origin and direction, then delivers only after the
  pending phase; death cancels it into the common resolution flash. The loader
  requires `windup_ms` and `telegraph` together and validates the exact geometry
  each of circle, cone, line, and ring consumes.
- `components.components` and `materials.materials` are empty. Slotted-blueprint
  crafting (Phase 2.3) and harvesting (Phase 4.1) fill them; the schemas and
  their validation exist now so those phases add data, not structure.
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
kinds must be unique across tables, and each class must have exactly one starter
weapon. Abilities add their own: the cost kind must be one the simulation can
charge, a magazine weapon's ability must spend ammunition it holds, every
applied effect must exist, an effect must be of a kind the world can run and may
only carry the fields its kind uses, and a damaging ability must declare a band
and an honoured dodge vector. Telegraph rows must pair with a positive windup,
carry positive active/resolved phase times, and use only their shape's geometry;
mob telegraph shapes come from the same vocabulary. Failures list every problem at once — run
`go test ./server/...`.
