# World structure

SpellFire is one contiguous world for 100+ concurrent players. A radial danger gradient controls risk and material grade; overlapping biomes control material type.

## Scale and traversal

The world is a circle of radius 22,500 units. Band radii are hub 450, Fringe 4,500, Frontier 15,750, and Deadlands 22,500, so the Frontier remains the widest band and the hub is a settlement footprint rather than a spawn ring.

**A journey from the hub to the rim takes roughly twice as long on foot as the straight line would.** That ratio is a property of the route, not of the radius: a straight radial line at base speed does not exist, so travel is funnelled through offset passes and detours. At the current halved world and raised player speed the median on-foot journey is about 105 seconds against a ~55-second straight line; the detour ratio, not the absolute figure, is what the traversal design guarantees. Impassable formations funnel radial travel through passes, hostile territory forces detours, and the direct approach is the dangerous one. Traversal time is therefore bought with terrain and risk, which is content, rather than with empty distance, which is not.

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

Biome-gated stock comes from two sources, so a region is worth travelling to whether or not anything is hunting in it: **aligned** material is taken from what lives there, and **growth** material is cut or quarried from the ground itself. Everything universal — structural stock, stave wood, and reagents — is available in every region, which is the concrete form of "geography never hard-locks a build". Grade is a **ceiling** rather than an equality: ground that yields Rare also yields everything below it, so walking outward adds options instead of trading them, and the reward curve's continuous value within a grade is what makes the outer reaches of a band worth more than its inner edge.

## Outposts and travel

Multiple safe outposts provide services and respawn points. Reaching an outpost once unlocks it for respawn. This is a navigation and survival gate, not a level gate. Outposts sit in the walkable annuli between the ridge belts, across the Fringe and Frontier and never in the Deadlands, and each declares which of loadout, crafting, and respawn it offers — a forward outpost may be a respawn point and nothing more, so the hub keeps a reason to exist.

On death a player is returned to the unlocked outpost **nearest to where they fell**, or to the central hub when that is closer. There is no destination menu: the walk back out is the penalty, and choosing where to reappear would turn dying into a routing decision. Unlocking outposts is what shortens that walk, which is the concrete reward for exploring.

**There is no fast travel of any kind.** No teleport between outposts, loaded or empty, and no exception for respawn — a respawn is a relocation to the nearest unlocked outpost, not a destination the player picks. Distance is the world's only currency for risk, and a teleport spends it for free.

The speed axis for repeat journeys is therefore **rideable entities**, not a movement state and not a teleport. A Gunslinger crafts a **vehicle** (a motorcycle); a Mage crafts a summoning crystal that materialises a **mount** (a warhorse). Both appear in the world when crafted — they are the product of the craft rather than an inventory item — and both are gated to an outpost that offers crafting, so a ride is prepared before a journey rather than conjured during one. A ride is transport only: no weapons or spells while riding. It has its own health, it takes the damage aimed at its rider, and when it is destroyed the rider is put back on foot. It cannot be mounted for a short window after dealing or taking damage, so it shortens a trip without becoming an escape from a fight. Multi-rider vehicles are future work; today one body rides one ride.

### Outpost safety

Exit camping is prevented by two locked rules:

- **No-PvP radius.** PvP is disabled inside a radius around every outpost, generously larger than the outpost footprint. Entering it does not protect a player from PvE or from consequences already in flight. This radius, not a distance from the origin, is what every safety rule now resolves against: the loadout lock, the crafting gate, the wall-placement rule, and both ends of PvP protection.
- **Exit invulnerability.** Leaving an outpost grants brief invulnerability that ends early on the player's own hostile action. It covers the transition out of the no-PvP radius, not a free approach to a fight. It is granted on the crossing itself, so standing inside safety never refreshes it and a body already outside cannot re-arm it without going back in.

Both durations and radii are tuning values. They must stay short and small enough that the safe bubble cannot be used offensively — a player cannot heal, reload, or rotate through it to win a fight they are losing outside it.

Outposts are fixed world fixtures: indestructible, uncapturable, and unaffectable by players. Capture, blockade, upgrades, and any other player influence belong to future territory design.

World presentation is defined in [`visual-direction.md`](visual-direction.md#world-rendering); hauling consequences live in [`economy-death-and-pve.md`](economy-death-and-pve.md).

## World items and fixtures

World geometry uses the common entity contract: mass, health, and one or more circle/box collision objects come from tuning and become mutable per-instance state. Procedural trees are immovable circular entities with 500 health and can be destroyed by projectiles. These substrate items are not the Mage's Stone wall, which remains a short-lived, player-authored and destructible spell with rewind-history obligations.

Each biome carries its own terrain archetypes — ridges, boulder fields, ruins, chasms, thickets, ice shelves, lava flows — and each declares independently whether it blocks movement, blocks vision, and stays readable beneath the shadow veil. Collision implies neither of the other two.

**The world is a procedural substrate plus an authored overlay.** The substrate is generated per chunk from the world seed and the region's parameters, materialised when a player approaches and evicted when none is near, so a world of this size is never fully resident. The overlay is what is placed by hand: outposts, points of interest, routes, and singular authored geography. The two are stored differently for the same reason they are edited differently — the substrate is reproducible from a seed and never enumerated, while the overlay is a finite list a designer maintains.

The map document holds the seed, the region parameters, the band radii, and the overlay. It is versioned JSON, exported and imported whole, and validated — coverage, geometry, and reachability — before any part of it is applied. It never contains materialised substrate.
