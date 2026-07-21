# Progression, crafting, and persistence

Progression grants access and build breadth while preserving a narrow combat power band. Three layers separate permanent achievement from fluid combat choices.

## Progression layers

| Layer | Persistence | Contents | Where it changes |
|---|---|---|---|
| Character | Permanent references | Level; part, spell, and keystone unlock ledger | Earned through XP, discovery, and materials |
| Crafting | Permanent items | Guns, staffs, special ammo, consumables | Safe zones only |
| Loadout | Fluid | One weapon plus equipped gadgets/spells and keystones | Safe zones only; locked outside |

The open-world loadout lock is the economy's keystone. Players commit before leaving safety and can be countered. Owning more options improves preparation, never the power carried into one encounter. Both classes can eventually unlock everything.

## Slotted-blueprint crafting

Guns and staffs share one system. A category blueprint exposes component slots; each option has material costs and behavioral effects.

| | Gun | Staff |
|---|---|---|
| Blueprint | Gun category | Staff category |
| Example slots | Muzzle, barrel, scope, trigger, magazine | Core, focus, conduit |
| Effects | Recoil, spread, capacity, range, handling | Cast speed, mana cost, projectile/area, element bias |
| Guardrail | Change handling and ceiling, not DPS band | Change behavior and ceiling, not damage band |

Crafting and spending occur only in safe zones; raw materials must be hauled there. Loadout respec is free or cheap, and every major balance patch grants a global respec/refund. Rebalancing must not invalidate a character.

## Persistence and versioning

Save ownership, not computed power:

- Crafted items store recipe and component IDs, not stat snapshots.
- Characters store unlock IDs and material counts, not derived HP or DPS.
- Versioned tuning tables derive all balance values at runtime.
- `schema_version` and sequential migrations handle structural changes only.
- Content changes are additive. Retired IDs remain resolvable and map to a replacement or refund.

Changing a tuning row updates every dependent item without character migration. Fluid loadouts make aggressive balance changes survivable.

This does not solve economy fairness. Source and sink changes can devalue past effort, so tune forward and never confiscate earned progress. See the global [`invariants.md`](invariants.md).
