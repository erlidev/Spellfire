# Progression, crafting, and persistence

Progression grants access, build breadth, and a bounded amount of raw power. Three layers separate permanent achievement from fluid combat choices.

## The vertical budget

Getting stronger is a real reward, not an illusion: rarer materials produce measurably better gear. The gain is finite and authored as a single pool rather than accumulated per item.

| Bound | Value | Meaning |
|---|---|---|
| Total vertical gain | **×2.0** effective combat power | Starter equipment → fully maxed, and no further |
| Damage share | ≤ ×1.45 | Applied to the [band](combat.md#damage-bands) anchor, never to cadence |
| Effective-health share | ≤ ×1.38 | Mitigation, shields, and armor combined |
| Per-item share | ≤ ⅓ of the total gain | No single equipped item decides a fight |
| Per-tier step | ≤ 26% effective power | One rarity step is unmistakable, not final |

The budget rides on **items**, not on the character sheet. Level gates which rarities and recipes a character may build; the numbers themselves come from the equipped item's components, so the [persistence contract](#persistence-and-versioning) still stores references and derives values from tuning tables.

### Rarity tiers

Item rarity follows the material grades the [world](world.md#biomes-type--grade) already produces, so where a player farms is what makes their gear better.

| Tier | Source | Effective power |
|---|---|---|
| Common | Fringe (T1) materials, starter kit | Baseline |
| Uncommon | Frontier (T2) materials | ≤ ×1.26 |
| Rare | Deadlands (T3) materials | ≤ ×1.59 |
| Signature | Rim world-boss rewards | ≤ ×2.00 |

Each tier must also spend at least half its value horizontally — a better sight, a wider magazine, a shorter cast, a new condition — so a Signature item plays differently from its Rare counterpart rather than being the same object with larger numbers. A tier that can only justify itself numerically is not authored.

### What the budget feels like

At equal gear, [raw TTK](combat.md#time-to-kill) is the locked three seconds. Across the full gap it moves to roughly **2.1 s** for the maxed player and **4.1 s** for the starter player — close to a two-for-one exchange. Between players of equal skill that is not a tilt, it is the result: the geared player wins, and is meant to. Overturning it takes a real skill gap plus something else — a landed opener from cover, a dodged signature, terrain, or a teammate — rather than merely playing better on the day.

Two consequences follow and are load-bearing:

- **The [danger bands](world.md#radial-danger) are the safeguard, not the tuning.** A 2× gap is only fair because the players holding it are pulled to the rim and PvP is off or restricted in the Fringe. A newcomer meeting a Signature build is a world-structure failure, not a balance one, and it is fixed in the reward curve rather than by shaving the budget.
- **The floor is absolute.** No gear combination may pull raw TTK below **2.0 s**, because damage arriving faster than a player can react to it stops being combat regardless of how it was earned. If the damage share and a burst band together threaten that floor, the band wins and the multiplier is capped.

## Progression layers

| Layer | Persistence | Contents | Where it changes |
|---|---|---|---|
| Character | Permanent references | Level; part, spell, and keystone unlock ledger | Earned through XP, discovery, and materials |
| Crafting | Permanent items | Guns, staffs, special ammo, consumables | Safe zones only |
| Loadout | Fluid | One weapon plus equipped gadgets/spells and keystones | Safe zones only; locked outside |

### Slots

The loadout is one **six-slot action bar**, the same width for both classes so a single binding set serves them:

| Class | Slot 1 | Slots 2–6 |
|---|---|---|
| Gunslinger | Equipped weapon | Five gadgets |
| Mage | Spell | Five more spells |

A Mage's staff is the delivery device rather than a bar slot: it casts whichever spell is selected. The counts live in [`loadout.json`](../../../data/tuning/loadout.json) and are tunable, but the two arrangements must stay the same width, because a class with bindings the other cannot reach would need its own control scheme. Keystones join the bar's owning table when [Phase 2.7](../../../TODO.md) settles them.

The open-world loadout lock is the economy's keystone. Players commit before leaving safety and can be countered. Owning more options improves preparation; owning rarer items improves power, but both are committed before the encounter and neither can be swapped in reply to what the fight turns out to be. Both classes can eventually unlock everything.

## Starter kit

A character with zero materials is combat-capable immediately, never a spectator waiting on drops. On creation a character receives:

- one basic weapon of their class — a starter gun or staff, drawn at random from the basic set;
- a small random selection of low-tier unlocks — gadgets for the Gunslinger, spells for the Mage — enough to fill a coherent loadout.

Randomization is deliberate. It gives two new players different opening tools without granting either an advantage, since every item in the kit is Common tier and shares the [effective damage band](gunslinger.md). It also makes the first hours a discovery of what a kit can do rather than a walk down one scripted path. Nothing in the kit is exclusive: every item in it is unlockable normally, so a bad draw is a starting flavor, not a permanent gap.

The kit defines the power floor — the zero point the whole vertical budget is measured from. A fully geared veteran must remain beatable by a clearly better player holding starter equipment; if a skill gap can no longer close it, the budget has been overspent and the tuning tables are wrong.

## Progression pacing

Material acquisition is the only gate between a new Gunslinger and every weapon, and a major Mage gate too. Drop rates are tuned to hit these targets:

| Milestone | Target | Meaning |
|---|---|---|
| First coherent crafted build | ~1 hour | A deliberate weapon plus matching loadout, not scavenged parts. ~⅓ of the vertical budget |
| Rim viability | ~10 hours | Able to survive and farm the Deadlands with a squad. ~⅔ of the vertical budget |
| Full Signature build | Long tail | The last third, item by item, from rim bosses |
| Skill ceiling | Far beyond | Mastery, not unlocks, is the remaining axis |

The first hour must feel like fast, legible progress; the tenth like arrival, not completion. The budget is front-loaded on purpose: most of the power a player will ever hold arrives while they are still learning the game, and the long tail is the last ~20% chased one Signature item at a time. That is what keeps the gap between a 10-hour player and a 500-hour one small enough to fight across — roughly ×1.2, not the full ×2 — while still making the 500 hours visible. The ×2 figure describes a maxed veteran against a character that has just been created, which is a matchup the [danger bands](world.md#radial-danger) are supposed to make rare. If a player reaches rim viability well under the target, drops are too generous and the mid bands become skippable; well over it, the middle turns into a grind instead of a route.

## Recipe-blueprint crafting

Guns and staffs use the same authoritative recipe system but present different workbenches.

| | Gun | Staff |
|---|---|---|
| Blueprint | One generic weapon outline | One two-part staff assembly |
| Required slots | Receiver, barrel, action, feed, sight | Mana crystal, stave |
| Result | The complete part arrangement determines the gun category | The crystal supplies effects; the stave supplies containment tier |
| Main constraint | Every slot must match one realistic authored gun recipe | Stave tier must be at least the crystal tier |

The nine gun recipes are explicit rather than inferred from vague tags: pistol, revolver, SMG, shotgun, service rifle, marksman rifle, sniper, LMG, and launcher each name the receiver, barrel, operating action, feed system, and sight types that can produce it. Some recipes accept a grounded alternative such as iron sights or a reflex sight, but two recipes may never accept the same complete arrangement. There is no free “stock craft”; every blank must be filled. Stock starter weapons still exist as starter-kit equipment, while crafting always represents building a new physical item.

A staff is made from exactly two crafted subassemblies:

- A **mana crystal** has its own material recipe and one all-spell effect. Current recipes cover cooldown/cast timing, mana efficiency, projectile or area shape, all-spell damage, and all-spell healing. Healing bonuses do not add healing to a spell that has none.
- A **stave** has no combat modifier. Its wood determines its containment tier: ash is tier 1, runed oak tier 2, and resonant ironwood tier 3. Higher recipes add tempered or resonant metal and magical infusions to the wood cost.

The UI presents crystal and stave crafting as two deliberate choices, then commits both material recipes atomically with the finished staff. Loose subassemblies are not inventory items: a refusal spends nothing, while success persists only the completed staff's crystal and stave references. This preserves the reference-only item contract and avoids an intermediate inventory that cannot be equipped or traded yet.

Components are where the [vertical budget](#the-vertical-budget) is actually spent. A component's tier sets both its material cost and the share of the budget it may draw, and validation caps the total a finished item can reach — the budget is enforced on the assembled weapon, not per part, or five modest parts would stack into an immodest gun.

Gun parts spend most of their value on handling and reach, and the remainder on the damage multiplier their tier allows. Mana crystals carry the Mage's equivalent: bounded `spell_damage` and `spell_healing` multipliers, general to every spell cast through the staff, which never alter a spell-specific row or the shared damage-band data itself.

Crafting and spending occur only in safe zones; raw materials must be hauled there. Loadout respec is free or cheap, and every major balance patch grants a global respec/refund. Rebalancing must not invalidate a character.

## Persistence and versioning

Save ownership, not computed power:

- Crafted items store recipe and component IDs, not stat snapshots.
- Characters store unlock IDs and material counts, not derived HP or DPS.
- Versioned tuning tables derive all balance values at runtime.
- `schema_version` and sequential migrations handle structural changes only.
- Content changes are additive. Retired IDs remain resolvable and map to a replacement or refund.

Changing a tuning row updates every dependent item without character migration. Fluid loadouts make aggressive balance changes survivable.

This does not solve economy fairness. Source and sink changes can devalue past effort, so tune forward and never confiscate earned progress. See the global [`invariants.md`](invariants.md).
