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
| `session.json` | Logout linger window and saved-position expiry |
| `world.json` | World radius, spawn radius, danger bands, procedural tree parameters |
| `combat.json` | Role and dodge-vector vocabularies, player body, universal dash, damage bands |
| `elements.json` | The five Mage elements and their roles |
| `weapons.json` | Craftable weapons; a staff delegates its combat numbers to the spell it casts |
| `spells.json` | Spells with element, tier, cost, cadence, dodge vector, and projectile |
| `components.json` | Blueprint slot layouts and the components that fill them |
| `materials.json` | Material grades, kinds, and rows |
| `mobs.json` | Mob contracts |
| `biomes.json` | Biomes and the element they align to |
| `outposts.json` | Recall destinations: outpost names and world positions |
| `retired.json` | Withdrawn IDs and the replacement or refund each resolves to |

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
weapon. Failures list every problem at once — run `go test ./server/...`.
