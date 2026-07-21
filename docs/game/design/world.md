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

Every outpost carries a no-PvP radius. The Deadlands has no outposts, so its nearest respawn is always a Frontier outpost — the rim's walk-back is the penalty, without a separate rule.

Bands are separated by incentive, not walls. Low-tier rewards should not interest veterans. A steep, visible transition warns new players before they approach veteran territory, while a convex reward curve pulls veterans to the rim and makes middle tiers a route rather than the best farm.

## Biomes: type × grade

Biomes and danger bands are independent:

- **Biome determines type.** A fire biome yields fire-aligned material and parts at every radius.
- **Radius determines grade.** The biome's outer reaches yield rarer versions.

This two-dimensional taxonomy gives every rare recipe a contestable geographic source. Structural and common materials remain available everywhere so geography never hard-locks a build.

## Outposts and travel

Multiple safe outposts provide services and respawn points. Reaching an outpost once unlocks it for respawn. This is a navigation and survival gate, not a level gate.

On death a player may choose any unlocked outpost or the central hub, then travels on foot from there; see [`economy-death-and-pve.md`](economy-death-and-pve.md#respawn). Unlocking outposts is therefore what buys redeployment range, which is the concrete reward for exploring.

Mounts and vehicles may create a chase/speed axis. Fast travel while carrying raw materials is forbidden because hauling risk cannot be teleportable. Respawn is not an exception to that rule: it is reached only by dying, which already forfeits the haul.

### Outpost safety

Exit camping is prevented by two locked rules:

- **No-PvP radius.** PvP is disabled inside a radius around every outpost, generously larger than the outpost footprint. Entering it does not protect a player from PvE or from consequences already in flight.
- **Exit invulnerability.** Leaving an outpost grants brief invulnerability that ends early on the player's own hostile action. It covers the transition out of the no-PvP radius, not a free approach to a fight.

Both durations and radii are tuning values. They must stay short and small enough that the safe bubble cannot be used offensively — a player cannot heal, reload, or rotate through it to win a fight they are losing outside it.

Outposts are fixed world fixtures: indestructible, uncapturable, and unaffectable by players. Capture, blockade, upgrades, and any other player influence belong to future territory design.

World presentation is defined in [`visual-direction.md`](visual-direction.md#world-rendering); hauling consequences live in [`economy-death-and-pve.md`](economy-death-and-pve.md).

