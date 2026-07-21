# Economy, death, and PvE

The economy turns gathered resources into a risky journey home. Nodes and mobs create different exposure, while death transfers most unbanked value into the world.

## Haul-to-craft loop

Raw materials can be crafted or spent only in safe zones. Unspent materials are at risk in transit; crafted gear and learned unlocks are safe. PvP therefore concentrates around farms, routes, and returning haulers.

## Death

| Kept | Lost |
|---|---|
| All crafted gear and weapons; a danger-tier-scaled share of carried materials | Most carried raw materials, dropped at the death location |

Insurance scales only with danger tier and never with squad size. Squads already split loot and reduce deaths; taxing group size would contradict the cooperation pillar. A geared player primarily loses position and the walk back from a safe outpost.

### Respawn

The respawn timer is roughly five seconds — long enough to register the loss, short enough that dying is not a break from playing. It does not scale with danger tier, gear, or killstreak; the walk back already scales with depth.

Respawn is a free-travel menu: the player picks any unlocked outpost or the central hub, with the nearest outpost to the death location offered as the default. Undiscovered outposts are never listed, so [reaching an outpost](world.md#outposts-and-travel) remains the only way to earn it as a destination and exploration keeps its reward.

| Choice | Purpose |
|---|---|
| Nearest unlocked outpost (default) | Return to the area, contest the drop, resume the trip |
| Any other unlocked outpost | Redeploy to a different biome or band without the overland walk |
| Central hub | Full reset: craft, restock, change loadout, pick a new direction |

Respawning is free. There is no durability, XP, currency, or material cost on top of the drop — death already transfers the haul, and stacking a second penalty would punish the players least able to absorb it.

Rim deaths need no special rule: the [Deadlands has no outposts](world.md#radial-danger), so no choice puts a player back on the rim.

### Death is not a travel route

Free-choice respawn makes dying the fastest way to cross the map, which brushes against the [no fast travel while carrying raw materials](world.md#outposts-and-travel) rule. The cost structure, not a special case, is what contains it:

- **Respawn selection requires death.** There is no voluntary teleport between outposts. Suicide to relocate pays the full material loss, so a loaded player who uses death as transit destroys the haul they were moving.
- **The insured share is small enough to be a consolation, not a shipment.** Insurance is tuned to soften a loss, not to make a death-teleport a profitable delivery. If players start dying deliberately to move goods, insurance is too generous at that tier — fix the rate, not the respawn menu.

A player carrying nothing may treat death as transit. That is acceptable: an empty-handed player carries nothing the economy protects, and the five-second timer plus the walk out from the chosen outpost keeps it from being frictionless.

## Logging out

Disconnecting is not an escape. The body stays in the world for ten seconds after the connection drops: it holds its ground, cannot move, dash, or fire, and remains a legitimate target the whole time. Someone who pulls the plug mid-fight can still be killed and still drops their haul, so combat logging saves nothing it would not have saved by standing still. Reconnecting inside the window resumes that same body wherever the fight has moved it — unless it was killed, in which case the death stands: the corpse is not resumed, and the next login enters at the hub like any other death.

The position a character logs out at is honoured for thirty minutes. Past that, the next login recalls it to the nearest safe fixture — an [unlocked outpost](world.md#outposts-and-travel), or the central hub when that is closer. The haul is untouched by the recall: a stale position costs the walk back out, never the materials, which keeps this a convenience rule rather than a second death penalty.

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

Mobs are distinct classes with their own bodies, AI, and combat. Behavior is per-class, not a shared template with swapped numbers: aggro, leashing, engagement range, and reaction to being kited are each mob's own contract. The first class is the Sentry; every future archetype requires its own.

### Sentry

Sentry is a family, not a single mob. Variants differ in turret count, movement, cadence, and preferred range while sharing the fused-turret silhouette and the contract below. Individual variants are defined as they are built.

The base Sentry is a mobile ranged attacker with a Diep.io-like body and fused rotating turret. Players hold separate weapons, so this fused silhouette identifies a mob at a glance.

A Sentry patrols around an origin. It acquires the nearest player within aggro range, drives toward a preferred range while aiming independently, and disengages beyond its leash. Players may kite it away, into rivals, or out of a chokepoint.

Its shot uses the standard [telegraph grammar](visual-direction.md#readability-system) and a dodgeable projectile—never hitscan. Difficulty rises by movement speed, cadence, telegraph speed, and turret count rather than per-shot damage inflation. Placement at nodes and chokepoints makes Sentries part of world structure: groups must clear, suppress, or bait them while watching for players.

Sentries inherit biome tint, hostile outline/ring, and a tier-readable turret silhouette.

**Deferred:** aggro radius, leash and reset rules, movement/turn speed, cadence, and turret progression carry no numbers yet. The behavior above is locked; values are set during implementation and fixed through playtesting, per variant. Specifying them before a Sentry exists in the build would be false precision.

