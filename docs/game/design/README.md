# SpellFire game design

These documents are the source of truth for SpellFire's game rules and their intent. Each file owns one topic; peer links supply shared context without duplicating rules. Player-facing presentation belongs in [`../ui/`](../ui/README.md), while implemented behavior belongs in [`../../architecture.md`](../../architecture.md).

The design fixes system shape, not false precision. Unless a value is explicitly locked, tune it in balance data. **Open** marks a known unresolved decision.

## Design map

- [`pillars.md`](pillars.md) — ordered product commitments
- [`combat.md`](combat.md) — TTK, damage bands, roles, mobility, and encounter dynamics
- [`classes.md`](classes.md) — shared class structure and matchup contract
- [`gunslinger.md`](gunslinger.md) — gunplay, weapon categories, defense, and progression
- [`mage.md`](mage.md) — spell counterplay, elements, the spell grid, affinity, and staffs
- [`progression-and-crafting.md`](progression-and-crafting.md) — progression layers, the vertical budget, rarity tiers, crafting, and persistence
- [`world.md`](world.md) — danger bands, biomes, outposts, and travel
- [`economy-death-and-pve.md`](economy-death-and-pve.md) — hauling, death, resources, and mobs
- [`squads-and-world-bosses.md`](squads-and-world-bosses.md) — cooperation, credit, and boss rewards
- [`visual-direction.md`](visual-direction.md) — procedural art language and readability
- [`invariants.md`](invariants.md) — rules every system must preserve
- [`open-decisions.md`](open-decisions.md) — unresolved and deliberately deferred work

