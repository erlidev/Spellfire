# Tuning tables

Versioned balance data. These files are the only place a balance number is
authored. The Go server embeds them (`data/data.go` → `server/internal/tuning`)
and the Vite client imports the same files (`web/src/tuning.ts`), so both sides
of the prediction loop read one source.

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
- **Every damaging row declares a dodge vector**, and no projectile may have
  zero travel speed. The loader rejects both.

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
| `simulation.json` | Tick/send rates, AOI radius, rewind window, interpolation delay |
| `world.json` | World radius, spawn radius, danger bands, procedural tree parameters |
| `combat.json` | Role and dodge-vector vocabularies, player body, universal dash, damage bands |
| `elements.json` | The five Mage elements and their roles |
| `weapons.json` | Craftable weapons; a staff delegates its combat numbers to the spell it casts |
| `spells.json` | Spells with element, tier, cost, cadence, dodge vector, and projectile |
| `components.json` | Blueprint slot layouts and the components that fill them |
| `materials.json` | Material grades, kinds, and rows |
| `mobs.json` | Mob contracts |
| `biomes.json` | Biomes and the element they align to |

## Deliberately empty and deliberately absent

Rows are populated only where a design document has settled them.

- `components.components` and `materials.materials` are empty. Slotted-blueprint
  crafting (Phase 2.3) and harvesting (Phase 4.1) fill them; the schemas and
  their validation exist now so those phases add data, not structure.
- `mobs.sentry` carries the settled contract — family, silhouette, damage band,
  dodge vector, turret count — and no aggro radius, leash, movement, or cadence.
  [`economy-death-and-pve.md`](../../docs/game/design/economy-death-and-pve.md#sentry)
  defers those to implementation and playtesting; writing numbers now would be
  false precision.
- `biomes` carry no placement. Biome geography is Phase 3.

## Validation

`server/internal/tuning` re-validates on every load: unknown JSON fields are
rejected, every cross-table reference is resolved, danger bands must run
outward from the hub to the rim with contiguous PvP protection, projectile
kinds must be unique across tables, and each class must have exactly one starter
weapon. Failures list every problem at once — run `go test ./server/...`.
