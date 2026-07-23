# World structure

SpellFire is one contiguous world for 100+ concurrent players. A radial danger gradient controls risk and material grade; overlapping biomes control material type.

## Scale and traversal

The world is a circle of radius 45,000 units. Band radii are hub 900, Fringe 9,000, Frontier 31,500, and Deadlands 45,000, so the Frontier remains the widest band and the hub is a settlement footprint rather than a spawn ring.

**A journey from the hub to the rim takes about five minutes on foot.** That figure is a property of the route, not of the radius: a straight radial line at base speed would take under three minutes, and no such line exists. Impassable formations funnel radial travel through passes, hostile territory forces detours, and the direct approach is the dangerous one. Traversal time is therefore bought with terrain and risk, which is content, rather than with empty distance, which is not.

This is a large world for its population. At 50 concurrent players the density is roughly one player per twenty screens of area, so **the design risk is emptiness, not crowding**. Nodes, outposts, routes, and mob placement are what concentrate players into contact, and are load-bearing rather than decorative. Expansion extends the rim, and the population that fills it is the reason to extend it.

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
- **Radius determines grade.** The biome's outer reaches yield rarer versions, and grade is what feeds the [rarity tiers](progression-and-crafting.md#rarity-tiers): depth is the vertical axis of progression, so getting stronger means taking more risk rather than putting in more hours.

This two-dimensional taxonomy gives every rare recipe a contestable geographic source. Structural and common materials remain available everywhere so geography never hard-locks a build.

Biome regions are generated procedurally from the world seed rather than authored: noise-warped regions over the five elements, blended at their borders instead of meeting at hard seams. Because the field is deterministic, "geography never hard-locks a build" is checked rather than trusted — the loader samples the field across every danger band and **refuses a seed that leaves an element absent from a band**, or whose mix in any band falls below the configured floor. A refused seed is re-rolled; it is never shipped and patched around.

A biome's identity reaches gameplay through three channels at once: which aligned materials it yields, which terrain archetypes generate in it, and its ambient palette. A player should be able to name the biome they are standing in without reading the HUD.

## Outposts and travel

Multiple safe outposts provide services and respawn points. Reaching an outpost once unlocks it for respawn. This is a navigation and survival gate, not a level gate.

On death a player may choose any unlocked outpost or the central hub, then travels on foot from there; see [`economy-death-and-pve.md`](economy-death-and-pve.md#respawn). Unlocking outposts is therefore what buys redeployment range, which is the concrete reward for exploring.

Mounts are the speed axis for repeat journeys at this scale, and they are a movement state rather than a vehicle: broken by damage and unenterable in combat, so they shorten a trip without removing the exposure that makes it one. Fast travel while carrying raw materials is forbidden because hauling risk cannot be teleportable. Respawn is not an exception to that rule: it is reached only by dying, which already forfeits the haul.

### Outpost safety

Exit camping is prevented by two locked rules:

- **No-PvP radius.** PvP is disabled inside a radius around every outpost, generously larger than the outpost footprint. Entering it does not protect a player from PvE or from consequences already in flight.
- **Exit invulnerability.** Leaving an outpost grants brief invulnerability that ends early on the player's own hostile action. It covers the transition out of the no-PvP radius, not a free approach to a fight.

Both durations and radii are tuning values. They must stay short and small enough that the safe bubble cannot be used offensively — a player cannot heal, reload, or rotate through it to win a fight they are losing outside it.

Outposts are fixed world fixtures: indestructible, uncapturable, and unaffectable by players. Capture, blockade, upgrades, and any other player influence belong to future territory design.

World presentation is defined in [`visual-direction.md`](visual-direction.md#world-rendering); hauling consequences live in [`economy-death-and-pve.md`](economy-death-and-pve.md).

## World items and fixtures

World geometry uses the common entity contract: mass, health, and one or more circle/box collision objects come from tuning and become mutable per-instance state. Procedural trees are immovable circular entities with 500 health and can be destroyed by projectiles. These substrate items are not the Mage's Stone wall, which remains a short-lived, player-authored and destructible spell with rewind-history obligations.

Each biome carries its own terrain archetypes — ridges, boulder fields, ruins, chasms, thickets, ice shelves, lava flows — and each declares independently whether it blocks movement, blocks vision, and stays readable beneath the shadow veil. Collision implies neither of the other two.

**The world is a procedural substrate plus an authored overlay.** The substrate is generated per chunk from the world seed and the region's parameters, materialised when a player approaches and evicted when none is near, so a world of this size is never fully resident. The overlay is what is placed by hand: outposts, points of interest, routes, and singular authored geography. The two are stored differently for the same reason they are edited differently — the substrate is reproducible from a seed and never enumerated, while the overlay is a finite list a designer maintains.

The map document holds the seed, the region parameters, the band radii, and the overlay. It is versioned JSON, exported and imported whole, and validated — coverage, geometry, and reachability — before any part of it is applied. It never contains materialised substrate.
