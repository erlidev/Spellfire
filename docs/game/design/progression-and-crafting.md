# Progression, crafting, and persistence

Progression grants access and build breadth while preserving a narrow combat power band. Three layers separate permanent achievement from fluid combat choices.

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

The open-world loadout lock is the economy's keystone. Players commit before leaving safety and can be countered. Owning more options improves preparation, never the power carried into one encounter. Both classes can eventually unlock everything.

## Starter kit

A character with zero materials is combat-capable immediately, never a spectator waiting on drops. On creation a character receives:

- one basic weapon of their class — a starter gun or staff, drawn at random from the basic set;
- a small random selection of low-tier unlocks — gadgets for the Gunslinger, spells for the Mage — enough to fill a coherent loadout.

Randomization is deliberate. It gives two new players different opening tools without granting either an advantage, since basic weapons share the [effective damage band](gunslinger.md) and low-tier unlocks sit inside the narrow combat power band. It also makes the first hours a discovery of what a kit can do rather than a walk down one scripted path. Nothing in the kit is exclusive: every item in it is unlockable normally, so a bad draw is a starting flavor, not a permanent gap.

The kit defines the compressed power floor. A fully crafted veteran build must remain beatable by a skilled player holding starter equipment; if it does not, the power band has drifted and the tuning tables are wrong.

## Progression pacing

Material acquisition is the only gate between a new Gunslinger and every weapon, and a major Mage gate too. Drop rates are tuned to hit these targets:

| Milestone | Target | Meaning |
|---|---|---|
| First coherent crafted build | ~1 hour | A deliberate weapon plus matching loadout, not scavenged parts |
| Rim viability | ~10 hours | Able to survive and farm the Deadlands with a squad |
| Skill ceiling | Far beyond | Mastery, not unlocks, is the remaining axis |

The first hour must feel like fast, legible progress; the tenth like arrival, not completion. After rim viability, additional time buys breadth and execution rather than power, which is what keeps the [narrow combat band](#progression-layers) honest. If a player reaches rim viability well under the target, drops are too generous and the mid bands become skippable; well over it, the middle turns into a grind instead of a route.

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

Gun parts primarily change handling and reach. Mana crystals are the narrow exception to the old behavior-only rule: bounded `spell_damage` and `spell_healing` multipliers may move spell output, but they remain general to every spell cast through the staff and never alter a spell-specific row or shared damage-band data.

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
