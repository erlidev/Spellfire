# Progression, crafting, and persistence

Progression grants access and build breadth while preserving a narrow combat power band. Three layers separate permanent achievement from fluid combat choices.

## Progression layers

| Layer | Persistence | Contents | Where it changes |
|---|---|---|---|
| Character | Permanent references | Level; part, spell, and keystone unlock ledger | Earned through XP, discovery, and materials |
| Crafting | Permanent items | Guns, staffs, special ammo, consumables | Safe zones only |
| Loadout | Fluid | One weapon plus equipped gadgets/spells and keystones | Safe zones only; locked outside |

The open-world loadout lock is the economy's keystone. Players commit before leaving safety and can be countered. Owning more options improves preparation, never the power carried into one encounter. Both classes can eventually unlock everything.

## Starter kit

A character with zero materials is combat-capable immediately, never a spectator waiting on drops. On creation a character receives:

- one basic weapon of their class — a starter gun or staff, drawn at random from the basic set;
- a small random selection of low-tier unlocks — gadgets for the Gunslinger, spells for the Mage — enough to fill a coherent loadout.

Randomization is deliberate. It gives two new players different opening tools without granting either an advantage, since basic weapons share the [effective damage band](gunslinger.md) and low-tier unlocks sit inside the narrow combat power band. It also makes the first hours a discovery of what a kit can do rather than a walk down one scripted path. Nothing in the kit is exclusive: every item in it is unlockable normally, so a bad draw is a starting flavor, not a permanent gap.

The kit defines the compressed power floor. A fully crafted veteran build must remain beatable by a skilled player holding starter equipment; if it does not, the power band has drifted and the tuning tables are wrong.

## Progression pacing

Material acquisition is the only gate between a new Gunslinger and every weapon, and a major Mage gate too. Drop rates are tuned to hit these targets:

| Milestone | Target | Meaning |
|---|---|---|
| First coherent crafted build | ~1 hour | A deliberate weapon plus matching loadout, not scavenged parts |
| Rim viability | ~10 hours | Able to survive and farm the Deadlands with a squad |
| Skill ceiling | Far beyond | Mastery, not unlocks, is the remaining axis |

The first hour must feel like fast, legible progress; the tenth like arrival, not completion. After rim viability, additional time buys breadth and execution rather than power, which is what keeps the [narrow combat band](#progression-layers) honest. If a player reaches rim viability well under the target, drops are too generous and the mid bands become skippable; well over it, the middle turns into a grind instead of a route.

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
