# World structure

SpellFire is one contiguous world for 100+ concurrent players. A radial danger gradient controls risk and material grade; overlapping biomes control material type.

## Radial danger

Danger rises from a safe central hub toward a lethal rim. Expansion extends the rim instead of rebuilding a dangerous center.

| Band | Danger | Materials | PvP | Purpose |
|---|---|---|---|---|
| Central hub | None | — | Off | Crafting, market, respawn, loadout |
| Fringe (T1) | Low | Common | Off or restricted | Safe new-player farming |
| Frontier (T2) | Medium | Uncommon | On | Main contested band |
| Deadlands (T3 rim) | High | Rare | Full | Heavy-weapon materials and bosses; no safe outposts |

Bands are separated by incentive, not walls. Low-tier rewards should not interest veterans. A steep, visible transition warns new players before they approach veteran territory, while a convex reward curve pulls veterans to the rim and makes middle tiers a route rather than the best farm.

## Biomes: type × grade

Biomes and danger bands are independent:

- **Biome determines type.** A fire biome yields fire-aligned material and parts at every radius.
- **Radius determines grade.** The biome's outer reaches yield rarer versions.

This two-dimensional taxonomy gives every rare recipe a contestable geographic source. Structural and common materials remain available everywhere so geography never hard-locks a build.

## Outposts and travel

Multiple safe outposts provide services and respawn points. Reaching an outpost once unlocks it for respawn. This is a navigation and survival gate, not a level gate.

Players may choose any unlocked outpost on respawn, then travel on foot. Mounts and vehicles may create a chase/speed axis. Fast travel while carrying raw materials is forbidden because hauling risk cannot be teleportable.

**Open:** prevent exit camping through brief exit protection, multiple exits, or both.

**Open:** outposts are contestable but indestructible. Capture, blockade, and upgrades belong to future territory design.

World presentation is defined in [`visual-direction.md`](visual-direction.md#world-rendering); hauling consequences live in [`economy-death-and-pve.md`](economy-death-and-pve.md).

