# Combat model

Combat is lethal when uncontested and durable only through player decisions. Both classes spend the same role budget through different mechanics; loadout choices create strengths and exposed answers.

## Time-to-kill

Unanswered damage kills in about **three seconds**. This raw TTK is locked and anchors the compressed stat band.

Effective TTK should be much longer because players mitigate, escape, reposition, break line of sight, and trade cooldowns. The gap between raw and effective TTK must come from skill.

The opener is therefore decisive. A solo build without mitigation or escape is non-functional, not merely weak. Low-defense builds remain valid when teammates provide peel, reinforcing [cooperative play](pillars.md#p3--cooperation-is-the-intended-strategy).

## Damage bands

A shotgun and a rifle cannot both deal the same damage per hit, but they must kill in the same three seconds. Damage is therefore authored on a small set of **bands**, and every damaging ability points at one. A band fixes damage per hit *and* the cadence that hit is allowed to arrive at, so the product lands on the locked TTK.

| Band | Shape | Used by |
|---|---|---|
| Sustained | Small hits, fast cadence | Automatic guns, tier-1 and tier-2 spells |
| Burst | Large hits, slow cadence, long recovery | Shotguns, revolvers, tier-3 spells |
| Heavy burst | Very large hits, hard commitment, punishing whiff | Snipers, launchers, tier-4 signatures |

Three rules keep this from becoming per-item damage authoring:

- **Bands are few and shared.** New content picks a band; it does not get one.
- **Every band resolves to the same TTK** within tolerance. The band test computes effective DPS from the band's damage and the ability's cadence, and fails any item that lands outside it. A higher band buys larger single hits and a worse miss, never faster killing.
- **Falloff and spread are band-compatible.** A shotgun reaches its band only at its intended range; a sniper reaches it only when scoped. Reducing conditions is how a heavy band stays fair, so range and accuracy conditions belong on the item, not the band.

## Shared combat roles

Balance equivalent roles across classes instead of comparing guns and spells ad hoc.

| Role | Purpose | Gunslinger | Mage |
|---|---|---|---|
| Damage | Sustained DPS | Automatic and mid-weight guns | Spammable low-tier spells |
| Burst | Front-loaded damage or execute | Snipers and shotguns | High-tier signatures |
| Control | Slow, root, stun, or knockback | Flashbangs and deployables | Frost and Earth |
| Mobility | Dash, blink, or speed | Dash and gadgets | Storm and movement spells |
| Sustain | Heal, shield, or regeneration | Armor and adrenaline | Arcane |
| Zone | Walls, traps, denial, or vision | Smoke, mines, deployables | Fire and Earth |
| Range | Engagement distance | Weapon class and optic | Element and spell choice |

Fixed equipped slots enforce the power budget. Progression adds safe-zone choices, never more power carried into one fight.

## Keystones

Keystones change behavior rather than enlarge numbers. A dash might empower the next spell but double its mana cost; a gun might overheat instead of reloading and lock after sustained fire. These tradeoffs create build identity under compressed progression.

## Universal dash

Every build has a short dash with a meaningful cooldown and no invulnerability frames. It can dodge one telegraph or adjust position, but cannot reliably escape or kite.

The dash is a fast burst of movement rather than a teleport: it carries the player along the direction chosen at the press over roughly an eighth of a second, cannot be steered mid-dash, and stops on trees and world bounds like normal movement. The travel time is short enough to feel decisive but long enough to be read and reacted to.

This floor prevents helpless builds at three-second TTK without erasing low-mobility archetypes. Genuine mobility still costs spell or gadget slots; players who decline it depend on teammates.

## Third parties and outnumbered fights

Frequent third parties and dangerous 1vX fights are intended in a 100-player world. Mobility and escape let players reset; knowing when to disengage is part of combat skill. Strong players can exploit terrain and ability windows to win outnumbered, but squads remain the safe default.

Class-specific delivery and counterplay are defined in [`classes.md`](classes.md), [`gunslinger.md`](gunslinger.md), and [`mage.md`](mage.md).

