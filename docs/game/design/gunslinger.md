# Gunslinger

The Gunslinger is a mechanical, aim-and-movement class. Mastery comes from aim, recoil control, cover, angles, and reload timing. All identity lives in the current loadout, which locks on leaving a safe zone.

## Gunplay

- Most guns fire dodgeable projectiles. Only snipers use hitscan.
- Every gun has recoil; moving while firing increases spread. Accuracy trades against mobility.
- Recoil is a fixed left/right pattern the muzzle *walks*, not a cone it is thrown into. Each shot steps the muzzle from where the last one left it, the first shot of a settled weapon is always true, and a quiet window settles it back to aim. The walked muzzle is drawn on the weapon and knocks the camera, so the pattern is something a player reads and compensates for rather than something they infer from missed shots.
- Guns use magazines and reloads. Ammo is effectively infinite except for crafted special ammunition such as rockets.
- Heavier classes impose more recoil, movement spread, or slowdown. They offer higher mastery payoff within the same effective damage band—not free damage.

## Weapon categories

Nine categories cover the armory. Weight class is the balance axis: it sets recoil, movement spread, and slowdown, and it never sets damage. Every category shares the [damage bands](combat.md#damage-bands), so a category is a set of conditions and a handling profile, not a power level.

| Category | Weight | Band | Role | Identity |
|---|---|---|---|---|
| Pistol | Light | Sustained | Damage | Fast reload, near-zero movement spread, small magazine. The mobile fallback. |
| Revolver | Light | Burst | Burst | Few rounds, heavy per-shot kick, slow recovery. Rewards a settled aim. |
| SMG | Light | Sustained | Damage | High rate of fire, tolerant of movement, steep falloff. Close range only. |
| Shotgun | Medium | Burst | Burst | A cone of pellets. Lethal at contact range, irrelevant beyond a dash or two. |
| Assault rifle | Medium | Sustained | Damage | The baseline. Automatic, mid-everything, no exposed condition. |
| Marksman rifle | Medium | Burst | Range | Semi-automatic and accurate standing, badly punished for firing on the move. |
| Sniper | Heavy | Heavy burst | Burst, Range | Hitscan to a cap when scoped, and only when scoped. See below. |
| LMG | Heavy | Sustained | Damage, Zone | Large magazine, spin-up, long reload, heavy slowdown. Suppression and area denial. |
| Launcher | Heavy | Heavy burst | Burst, Control | Ground-indicator area damage with knockback, fed by finite crafted rockets. |

Categories are not unlocked in a power order. A pistol is a valid rim loadout; heavy categories cost rare materials because they demand commitment, not because they win fights ([`invariants.md`](invariants.md)).

## Snipers

A sniper round is hitscan to a weapon-specific cap, then becomes a travel-time projectile with falloff and a hard maximum range.

Effective fire requires scoping. The scope exposes only a controllable area near the player and blacks out peripheral vision. Hitscan is balanced by this committed vulnerability window.

## Defense

Military equipment covers two needs:

- **Vision and aim disruption:** smoke breaks line of sight; flashbangs disrupt aim. These excel against other Gunslingers but are weaker against forgiving, ground-targeted magic.
  - A **smoke canister** is thrown and leaves a standing cloud for a fixed window. Anyone on the far side of it stops being visible at all — the server stops sending them — while two bodies close enough to touch still see each other, so a cloud is cover rather than a personal blindfold. It blocks nothing physically: bullets, blasts, and bodies pass straight through.
  - A **flashbang** is thrown and goes off as an area that takes vision *whole* for a short window, without dealing damage or slowing anyone. Its counterplay is the travel time of the canister and the ground it covers, which is why it disrupts a fight rather than deciding one.
  - Both carry long cooldowns of their own on top of the shared cadence: they are openers and disengages, not a rate of fire.
- **Burst denial:** the universal dash and a deployable riot shield answer a Mage opener.

The riot shield blocks only a frontal arc, slows its user, and prevents firing while raised. It blocks frontal bullets and projectiles, not ground effects placed behind or beneath it. These costs preserve the cross-class bait-and-punish fight: Mages dodge bullets, Gunslingers dodge telegraphs, and both try to draw out the other's hard stop.

## Progression

The Gunslinger has no skill tree or handling attributes. A flat permanent ledger records gun-part and blueprint unlocks earned through level or discovery.

Rare materials gate heavy weapons economically. Heavy weapons remain situational loadout choices and must share the starter weapon's effective damage band; rarity buys commitment and skill ceiling. Without a skill-tree gate, progression pace depends almost entirely on material drop rates, making [economy pacing](progression-and-crafting.md#progression-pacing) a critical tuning problem.

Gun assembly uses the shared [`progression-and-crafting.md`](progression-and-crafting.md#recipe-blueprint-crafting) system. The finished category is resolved from a complete receiver, barrel, action, feed, and sight arrangement rather than selected independently of its parts.
