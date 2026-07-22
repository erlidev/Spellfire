# Mage

The Mage is a cognitive class built around timing, prediction, spacing, and resource economy. Every spell is eventually unlockable; the safe-zone loadout supplies all specialization.

## Dodgeability is mandatory

Every damaging spell needs at least one counterplay vector: telegraph, cast time, projectile travel, or delayed ground indicator. Forgiving aim may use large hitboxes, assistance, area effects, or lock-on with travel time, but never instant point-and-click damage. Without a dodge window, the cross-class matchup collapses.

## Mana and cooldowns

- Regenerating **mana** limits sustained, low-tier casting. Emptying it creates vulnerability.
- Per-spell **cooldowns** limit defining, high-tier spells.

Managing both axes replaces the Gunslinger's magazine and recoil burden. Spending every cooldown and missing should expose a Mage as clearly as an empty magazine exposes a Gunslinger.

## Elements

Each school owns a primary role and enough secondary utility to support specialization.

| Element | Primary role | Secondary tool | Character |
|---|---|---|---|
| Fire | Sustained damage and zoning | Burn/DoT denial | Attrition and space control |
| Frost | Control | Light mitigation | Lockdown and peel |
| Storm / Lightning | Burst | Short mobility | Assassination and repositioning |
| Arcane | Sustain and utility | Shields, dispel, teleport, mana | Support and enablement |
| Earth / Stone | Zoning and heavy mitigation | Walls, knockback, armor | Anchoring and denial |

No element owns pure ranged poke, and the set is finalized without one. Poke rewards attrition at a distance where neither player is committed, which works against the class's timing-and-prediction identity and against the [combat pillars](combat.md). The Gunslinger already holds sustained ranged pressure; duplicating it on the Mage would blur the matchup contract. If playtesting shows the Mage lacks a way to open a fight at range, a poke tool can be added later as a secondary on an existing element rather than a sixth school.

## Element affinity

Affinity encourages specialization without requiring it. The rule's shape is locked; its counts remain tunable: to equip a tier-N spell, a loadout must contain N−1 other spells of that element.

With six slots, the current examples are concise:

- **4 + 2:** one tier-4 signature plus utility; legible and counterable.
- **3 + 3:** two tier-3 identities.
- **2 + 2 + 2:** a flexible tier-2 generalist with no signature.

Higher tiers mean more mana, cooldown, telegraph, payoff, and whiff punishment. Tier is a commitment axis, not the vertical one: a tier-4 signature and a tier-1 spam spell both scale with the staff's crystal, so a Mage's power comes from what they crafted, and their build identity from what they equipped. A player who dodges the signature owns its downtime window.

## The spell grid

Every element is authored to tier 4. This is a floor, not a flourish: affinity requires N−1 same-element spells to equip a tier-N spell, so an element with fewer than four spells cannot support the 4 + 2 build its own rule describes.

| | Tier 1 — spam | Tier 2 — secondary | Tier 3 — identity | Tier 4 — signature |
|---|---|---|---|---|
| **Fire** | **Fire bolt** — traveling bolt, applies burn | **Cinder patch** — placed ground indicator leaving a burning area | **Flame wave** — telegraphed cone, stacks burn across a group | **Firestorm** — large delayed area, denies ground for its duration |
| **Frost** | **Frost shard** — traveling shard, applies slow | **Rime ward** — self mitigation and a chilling aura | **Ice trap** — placed indicator that roots whoever triggers it | **Blizzard** — wide zone whose stacking slow ends in a stun |
| **Storm** | **Spark** — fast, low-damage bolt | **Thunderstep** — short blink after a cast time | **Chain lightning** — cast time, then arcs between nearby targets | **Skyfall** — heavy ground indicator; landing it grants a blink |
| **Arcane** | **Arcane missile** — homing, but always with travel time | **Ward** — absorbing shield on self or an ally | **Nullify** — strips effects and shields, returns mana | **Rift** — paired teleport repositioning the caster and squad |
| **Earth** | **Stone shard** — slow, heavy bolt with knockback | **Stone wall** — a placed, destructible solid barrier | **Upheaval** — ground indicator, knockback and a brief root | **Bulwark** — armor to the caster and nearby allies, plus a shockwave |

Tier sets cost and commitment, not the vertical step: higher tiers spend more mana, hold longer cooldowns, telegraph longer, and leave a worse whiff. Tier 1 is mana-gated and always available; tier 4 is cooldown-gated and defines the window a dodging opponent earns. The [rarity of the staff](#staffs) is what moves output, and it moves every row of this grid together.

Each row carries at least one counterplay vector — travel time, cast time, telegraph, or ground indicator — because the [ability schema](../../architecture.md#abilities-and-effects) rejects a damaging spell without one. Non-damaging rows (Rime ward, Ward, Nullify, Rift, Stone wall) are exempt from the requirement, and pay their cost in mana and cooldown instead.

### Stone wall

The wall is the only spell that is not just a table row. It creates the first dynamic, player-authored collider, building on the common entity/collision substrate already used by destructible trees and fixed walls, and it carries obligations the rest of the grid does not:

- It is a **short-lived, destructible** span of segments placed perpendicular to the caster's aim. Damage destroys it early; it expires on its own otherwise.
- It blocks movement, projectiles, and line of sight through the shared terrain-occlusion rule, and it never blocks ground-placed area effects — the same exemption the [riot shield](gunslinger.md#defense) carries.
- Because the server rewinds to resolve hits, a wall's **lifetime is part of the rewind history**. A shot rewound to a moment when the wall stood must be blocked by it.
- One wall per caster, and it may not be placed overlapping an actor or inside a safe zone. Safety is never an offensive tool, and a wall that boxes a player in would make it one ([`invariants.md`](invariants.md)).

## Staffs

Staffs use the shared [recipe-blueprint system](progression-and-crafting.md#recipe-blueprint-crafting). Each staff is exactly one crafted mana crystal and one stave: crystals apply general effects such as cast timing, mana efficiency, spell damage/healing, or projectile/area shape, while wood and infusions determine the stave tier required to contain stronger crystals.

The staff is the Mage's whole share of the [vertical budget](progression-and-crafting.md#the-vertical-budget) — the counterpart to the Gunslinger's gun parts — because spells themselves are unlocks rather than items. A rarer crystal must therefore also earn its horizontal half: a shorter cast, a wider area, a cheaper cost, not only a larger `spell_damage` figure.
