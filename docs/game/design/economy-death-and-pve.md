# Economy, death, and PvE

The economy turns gathered resources into a risky journey home. Nodes and mobs create different exposure, while death transfers most unbanked value into the world.

## Haul-to-craft loop

Raw materials can be crafted or spent only in safe zones. Unspent materials are at risk in transit; crafted gear and learned unlocks are safe. PvP therefore concentrates around farms, routes, and returning haulers.

## Death

| Kept | Lost |
|---|---|
| All crafted gear and weapons; a danger-tier-scaled share of carried materials | Most carried raw materials, dropped at the death location |

Insurance scales only with danger tier and never with squad size. Squads already split loot and reduce deaths; taxing group size would contradict the cooperation pillar. A geared player primarily loses position and the walk back from a safe outpost.

**Open:** respawn timer and whether a rim death forces a distant respawn.

## Dropped materials

Materials are exclusive to the killer's squad for 30 seconds, then free-for-all until they despawn after 15 minutes. Exclusivity protects the squad from outside scavengers, never one squadmate from another. Pickup is unrestricted by unlocks; crafting still enforces blueprint requirements.

This intentionally makes solo rim hauling harsh. High-tier insurance is the tuning lever if too much value flows from soloists to gankers.

## Resource sources

| | Nodes | Mobs |
|---|---|---|
| Action | Hold position to harvest | Fight a mobile target |
| Exposure | Vulnerable channel, interruptible at any time | Active combat |
| Materials | Basic, general, structural | Rare, specific, type-aligned |
| Scaling | Higher grade takes longer | Higher tiers demand harder execution |
| PvP pressure | Contested fixed locations | Interdiction and hunting |

Both sources are required. A farming trip should mix mining and combat. Long deep-zone harvests create a natural “one gathers, one watches” squad role.

## Enemy classes

Mobs are distinct classes with their own bodies, AI, and combat. The first is the Sentry; future archetypes require separate contracts.

### Sentry

The Sentry is a mobile ranged attacker with a Diep.io-like body and fused rotating turret. Players hold separate weapons, so this fused silhouette identifies a mob at a glance.

A Sentry patrols around an origin. It acquires the nearest player within aggro range, drives toward a preferred range while aiming independently, and disengages beyond its leash. Players may kite it away, into rivals, or out of a chokepoint.

Its shot uses the standard [telegraph grammar](visual-direction.md#readability-system) and a dodgeable projectile—never hitscan. Difficulty rises by movement speed, cadence, telegraph speed, and turret count rather than per-shot damage inflation. Placement at nodes and chokepoints makes Sentries part of world structure: groups must clear, suppress, or bait them while watching for players.

Sentries inherit biome tint, hostile outline/ring, and a tier-readable turret silhouette.

**Open:** aggro radius, leash and reset rules, movement/turn speed, cadence, and turret progression. The behavior above is locked; values are not.

