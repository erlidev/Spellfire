# SPELLFIRE

Game Design Document

*A 2D top-down open-world combat MMORPG in the .io tradition*

Version 0.1 — Systems Design Draft

*Status: core systems locked · balance values and second-order features open*

**Contents**

## 0. Reading this document

**Scope.** This document specifies SpellFire's *systems and their design intent* — the rules of the game and why each rule exists. It deliberately contains almost no final numbers. Where a mechanic is locked but its value is not (time-to-kill windows aside), the value is marked *to tune*. A GDD fixes the shape of a system; a balance spreadsheet, produced later, fixes its values. Committing fake-precise numbers here would imply decisions that have not been made.

**Status tags.** Sections or lines marked **OPEN** are known-unresolved design areas, not oversights. Amber callout boxes flag either an open decision or an *accepted tradeoff* — a known imperfection the team has chosen on purpose. These are collected again in §11.

**Design pillars.** Every system below is justified against the four pillars in §1. When two desirable features conflict, the pillars break the tie. If a future change violates a pillar, that is the signal to reconsider the change, not the pillar.

## 1. Design pillars

Four commitments constrain every downstream decision. They are ordered; when they conflict, the earlier pillar wins.

#### P1 — Skill and tactics beat levels

A level-20 player must be able to defeat a level-80 player through superior aim, movement, prediction, positioning, or a unique tactical answer. Character level buys options and skill ceiling, never a decisive statistical advantage. This is the identity of the game and the hardest pillar to keep.

#### P2 — Compressed vertical progression

Core combat stats (HP, base damage) are held inside a narrow band across the entire level range so that time-to-kill stays roughly constant regardless of level. Progression is overwhelmingly *horizontal*: it grants breadth — more weapons, more spells, more build permutations, more counters to choose from — not bigger numbers. A maxed character has more *answers*, not more power.

#### P3 — Cooperation is the intended strategy

The world is tuned so that squads outperform soloists in the areas worth contesting. Soloing the most dangerous content stays possible for the strongest players but is never the optimal path. Economy, difficulty, and reward rules all nudge toward grouping; none should punish it.

#### P4 — An expandable, seamless world

One contiguous open world, not instanced zones. Its structure must allow the live map to grow over time without rebuilding existing content — which is why danger increases outward from a safe centre (see §7).

> **Load-bearing tension (resolved).** “Deeper progression” and “vertical scaling shouldn’t matter” are in natural opposition. SpellFire resolves this by defining progression as *access and skill ceiling*, never raw power. Every ‘stronger’ item, spell tier, or heavy weapon in this document means *higher skill ceiling and higher commitment at the same effective power band* — never a larger number. This single rule recurs throughout; it is the mechanism that keeps P1 and P2 true.

## 2. Combat model

### 2.1 Time-to-kill: raw vs. effective

**Raw TTK ≈ 3 seconds.** With no counterplay — one player freely damaging another — a kill takes about three seconds. This is the *locked* reference the compressed stat band is built around.

**Effective TTK is much longer** and is produced entirely by player agency: mitigation, escapes, repositioning, breaking line of sight, and forcing cooldown trades. The whole combat design is the discipline of keeping the gap between raw and effective TTK *skill-driven*. A player who uses their tools well survives many multiples of 3s; a player who does not dies in 3s.

> **Accepted consequence — the opener is decisive.** At 3s raw TTK, whoever lands a clean first burst wins *unless the target has a defensive or escape tool available*. Mitigation/escape is therefore not optional: a build with no defensive answer is not ‘weak’, it is non-functional solo. Weak-defensive builds are *team-dependent by design* (see §4, §5) — they are enabled by teammates who peel, not played alone.

### 2.2 The seven combat roles (shared power budget)

Both classes express the same seven roles through different means. Balance is done per-role across classes — comparing how each class delivers Control, or Burst — rather than ad hoc gun-versus-spell. This shared vocabulary is what keeps two asymmetric classes on a comparable power budget.

| **Role** | **Definition**                       | **Gunslinger expression**      | **Mage expression**        |
|----------|--------------------------------------|--------------------------------|----------------------------|
| Damage   | Sustained DPS                        | Automatic / mid-weight guns    | Spammable low-tier spells  |
| Burst    | Front-loaded / execute               | Snipers, shotguns              | High-tier signature spells |
| Control  | Slow / root / stun / knockback       | Flashbangs, deployables        | Frost, Earth               |
| Mobility | Dash / blink / speed                 | Dash + mobility gadgets        | Storm, movement spells     |
| Sustain  | Heal / shield / regen                | Armor / adrenaline consumables | Arcane                     |
| Zone     | Walls / traps / area denial / vision | Smoke, mines, deployables      | Fire, Earth                |
| Range    | Effective engagement distance        | Weapon class / optic           | Element / spell choice     |

**Budget enforcement is by equipped slots, not stat caps.** Progression unlocks more *options to choose between*, not more slots to fill. Because equipped slot counts are roughly fixed and matched in total combat value across classes, ‘breadth’ means *you own more counters to pick from in the safe zone* — never that you carry more power into a fight.

### 2.3 Keystones — build identity without power creep

Keystones are modifiers that change *how* something works, never how large a number is. Examples: “your dash empowers your next spell but doubles its mana cost”; “your gun overheats instead of reloading — no reload, but sustained fire locks you out.” Under compressed vertical, keystones (not stats) are the primary source of build differentiation for both classes.

### 2.4 Universal dash — a floor, not a solution

Every build has a baseline dash by default. It is deliberately weak: short distance, meaningful cooldown, no invulnerability frames — enough to help dodge a telegraph or reposition slightly, never enough to reliably disengage a lost fight or kite.

- **Why a floor:** guarantees no build is unplayably helpless at 3s TTK.

- **Why weak:** preserves the low-mobility archetype (P3). A build can choose to run only the baseline dash and lean on teammates for repositioning.

- **Real mobility is still an investment:** Storm/movement spells and mobility gadgets stack on top of the baseline and are what make a genuinely mobile build.

### 2.5 Third-party and 1vX reality

> **Accepted dynamic.** In a 100-player shared world, a ~3s raw / long effective fight will frequently be third-partied, and being outnumbered is dangerous. This is intended texture, not a bug. The counter is *disengagement*: mobility and escape tools let a losing or outnumbered player reset. Skill therefore includes *knowing when to break off*, not only aim. 1vX is possible for a strong player exploiting ability windows and terrain, but is never the safe default — which is what makes squads (P3) matter.

## 3. Classes — shared structure, asymmetric feel

SpellFire has a hard class division: Gunslingers and Mages. There is no cross-class hybridisation. The two classes are now mirror-structured in progression (both eventually unlock everything and specialise through fluid safe-zone loadouts) but deliberately asymmetric in combat feel.

| **Axis**                 | **Gunslinger**                 | **Mage**                         |
|--------------------------|--------------------------------|----------------------------------|
| Skill type               | Mechanical — aim + movement    | Cognitive — timing + prediction  |
| Combat model             | Cover / positioning shooter    | Telegraph / dodge caster         |
| Aim requirement          | High (Overwatch-like)          | Low (forgiving; lead/predict)    |
| Downtime / punish window | Reload, recoil, mag management | Mana depletion, cooldowns        |
| Specialisation axis      | None enforced — free mix       | Element affinity (loadout rule)  |
| Build visibility         | Hidden until used              | Self-advertising (element-heavy) |
| Permanent identity       | None — fluid armory            | None — fluid armory              |

> **Known permanent tuning burden.** Because the classes are asymmetric in feel, the team effectively balances three matchups: Gunslinger v Gunslinger, Mage v Mage, and Gunslinger v Mage. The cross-class matchup will always be the sorest, and its balance is skill-dependent: in low-skill play mages feel oppressive (no aim tax); in high-skill play gunslingers pull ahead (they dodge telegraphs *and* convert aim fully). Expect a persistent new-player perception that mages are strong and gunslingers are hard. This is an intended skill matchup, tracked as an ongoing tuning cost.

**Build-visibility asymmetry (intentional)**

Element affinity makes mage builds partly readable in advance — an opponent sees a fire-heavy mage and can pre-plan dodges. Gunslinger builds stay hidden because tools are mixed freely and only revealed on use. Mages are more counter-buildable; gunslingers surprise. This is a fair trade against the mage’s lower aim tax and a knob to lean on if the matchup skews.

## 4. The Gunslinger

A mechanical, aim-and-movement class in the Overwatch mould. Combat skill lives in aim, recoil control, cover use, angles, and timing reloads. The Gunslinger is a fluid armory: they unlock guns and parts permanently but carry no permanent build identity — all identity is in the current loadout, which locks the moment they leave a safe zone.

### 4.1 Gunplay fundamentals

- **Projectiles, not hitscan, as the rule.** Most guns fire dodgeable projectiles, to make full use of the 2D top-down space and to keep movement/aim central (P1). Hitscan is reserved for snipers only.

- **Recoil on all guns; move-spread.** Every gun has recoil, and spread increases while firing on the move. Standing to shoot trades mobility for accuracy — a constant micro-decision.

- **Magazines, mostly infinite ammo.** Each gun has a magazine size and reloads. Ammo is effectively infinite except for a small set of special-ammo weapons (e.g. RPG rockets) whose ammo must be crafted.

- **Weight classes.** Guns fall into weight classes; heavier guns carry harsher recoil and move-spread — higher skill ceiling, not higher damage (see §4.4).

### 4.2 Snipers (the hitscan exception)

- **Hitscan to a capped range, then projectile.** A sniper round is instant up to a per-weapon range cap, then becomes a travel-time projectile beyond it, with damage falloff and a hard maximum range.

- **Scoping in.** Firing effectively requires scoping, which blacks out the screen except a manipulable scope view around a limited area near the body — real-scope handling. This is the sniper’s self-imposed vulnerability window: while scoped, peripheral awareness is gone.

### 4.3 Defensive toolkit

Military equipment is the Gunslinger’s kit. Two categories, with distinct roles at 3s TTK:

- **Vision/aim counterplay (intra-class strength):** smoke (breaks line of sight), flashbangs (disrupt aim). Strong Gunslinger-v-Gunslinger; weak against mages, whose ground-targeted telegraphs often ignore LOS and whose forgiving aim shrugs off flash.

- **Hard-stop defense (anti-burst, required):** a deployable **riot shield** plus the universal dash. The shield is the answer to a mage’s opening burst that flash/smoke cannot provide.

> **Riot shield must stay directional and committing.** It blocks a frontal arc only, slows the user, and locks their fire while raised. It stops bullets and projectiles from the front, but not ground-AoE dropped behind or beneath it. If it ever becomes a costless deploy-and-ignore wall, the Gunslinger-v-Mage skill test collapses into a free ‘ignore the mage’ button. Directional + movement-penalty + fire-lock is the honest version.

**Resulting matchup (intended):** mage dodges bullets, gunslinger dodges telegraphs, each holds one hard-stop to bait out. A clean, symmetric, skill-decided fight.

### 4.4 Progression: unlock ledger + material-gated heavy weapons

The Gunslinger has no skill tree (a deliberate ‘separate experience’ from the Mage). Their permanent layer is a flat unlock ledger of gun parts and blueprints, plus a purely economic gate on heavy weapons. There is no handling stat or attribute investment — that axis was removed.

- **Part unlocks:** levelling and discovery unlock new gun parts and blueprints to craft with. A flat list, no branching.

- **Heavy weapons are gated by rare materials, not by a stat.** Obtaining the rare mats to craft a heavy gun is the gate. Because heavy guns are loadout-swappable like everything else, a heavy gun is a *situational pick*, not a committed identity.

<table>
<colgroup>
<col style="width: 100%" />
</colgroup>
<tbody>
<tr class="odd">
<td><p><strong>Critical guardrail — rarer materials buy skill ceiling, never power.</strong> A heavy or rare-material gun must sit in the <em>same compressed effective-DPS band</em> as a starter gun: harder to control (recoil, move-spread, weight/slow), with higher payoff when mastered, not a bigger number. If rare-mat guns are simply stronger, ‘farm rare mats → win’ becomes vertical power by another name and violates P1/P2.</p>
<p><strong>Consequence to accept — pacing is now pure economy tuning.</strong> With no skill-tree gate, material grind is the <em>only</em> thing between a new player and every weapon. Gunslinger progression pace is therefore entirely a drop-rate problem (see §11, pacing).</p></td>
</tr>
</tbody>
</table>

## 5. The Mage

A cognitive class. Combat skill lives in timing, prediction, spacing, and resource/cooldown economy rather than aim. The Mage’s ‘aim’ is leading and predicting where the enemy will be when a telegraph resolves. Like the Gunslinger, the Mage is a fluid armory: every spell is eventually unlockable, and specialisation is expressed purely through the equipped loadout.

> **The load-bearing mage rule — no instant, point-and-click damage.** Every damaging spell must have at least one dodge vector: a telegraph, a cast time, a projectile travel time, or a ground-indicator delay. ‘Low aim requirement’ means *forgiving* aim (large hitboxes, target assist, area effects, lock-on-with-travel-time), never *no counterplay*. The Gunslinger’s entire answer to a mage is dodging and breaking LOS; if mage damage were instant, that answer vanishes and the matchup collapses.

### 5.1 Resource model: mana + cooldowns

Two independent resource axes give the Mage the punishable downtime a Gunslinger gets from mags and reloads:

- **Mana pool (regenerates over time):** governs spammable / sustained low-tier spells. Running dry is the vulnerable window.

- **Individual cooldowns:** govern big, defining, high-tier spells. Managing pool and cooldowns simultaneously is the cognitive-skill layer that substitutes for aim.

A mage who dumps cooldowns and whiffs is exposed exactly as a gunslinger who empties a magazine — symmetric punishable downtime, which is what keeps the classes comparable.

### 5.2 Elements (schools) and their roles

Five elements, each owning a primary combat role plus a secondary tool so no single-element specialist is completely one-dimensional. Element identity makes the specialist↔generalist choice a real tactical decision, not flavour.

| **Element**       | **Primary role**        | **Secondary tool**                      | **Character**            |
|-------------------|-------------------------|-----------------------------------------|--------------------------|
| Fire              | Sustained damage + Zone | Area denial (burn/DoT)                  | Attrition, space control |
| Frost             | Control                 | Light mitigation                        | Lockdown, peel           |
| Storm / Lightning | Burst                   | Short-range mobility (blink on hit)     | Assassinate, reposition  |
| Arcane            | Sustain / utility       | Shields, dispel, teleport, mana economy | Support, enable          |
| Earth / Stone     | Zone + heavy mitigation | Walls, knockback, armor                 | Anchor, deny             |

**OPEN —** No element currently owns a pure ranged-poke identity. Decide whether that is a genuine gap to fill (a sixth element, or a poke sub-branch) or an intentional absence.

### 5.3 Element affinity — specialisation without forcing it

Builds are never forced to specialise, but higher-tier spells of an element are gated behind equipping enough of that element. This creates specialisation pressure while leaving generalising legal (capped at low tiers).

<table>
<colgroup>
<col style="width: 100%" />
</colgroup>
<tbody>
<tr class="odd">
<td><p><strong>Proposed loadout rule (shape locked, numbers to tune).</strong> Spells have tiers 1–4. To equip a tier-N spell, the loadout must also include (N−1) other spells of the same element. With 6 spell slots:</p>
<ul>
<li><p><strong>4 + 2</strong> → one tier-4 signature + utility. Hard specialist; most legible, most counterable.</p></li>
<li><p><strong>3 + 3</strong> → two elements capped at tier-3. Balanced dual identity.</p></li>
<li><p><strong>2 + 2 + 2</strong> → three elements capped at tier-2. Generalist; no signature, hardest to hard-counter, no high-ceiling payoff.</p></li>
</ul></td>
</tr>
</tbody>
</table>

> **Tier semantics (locked) — higher tier = higher commitment and skill ceiling, NOT higher raw power.** A tier-4 spell has a longer cooldown, higher mana cost, and a bigger/slower (more dodgeable) telegraph, with a large payoff if it connects and a large punish window when it whiffs or sits on cooldown. A level-20 dodges the tier-4 specialist’s signature and wins the window it is unavailable. Tier and resource cost reinforce each other: low-tier is mana-cheap and spammy; high-tier is cooldown-gated and mana-heavy — a committed, punishable ‘ultimate’. This is the mechanism that lets the tier ladder exist without violating P1/P2.

### 5.4 Staffs

Staffs use the *same slotted-blueprint crafting system as guns* (see §6), skinned differently: a staff blueprint with slots (core, focus, conduit, …), each component modifying spell *behaviour* rather than raw power — e.g. +cast speed to one element, −mana cost, larger projectiles/AoE, or a keystone-like modifier (‘first spell after a dash is empowered’). Staffs shape and specialise; they do not inflate damage. One crafting system, built once, skinned twice.

## 6. Progression & crafting

### 6.1 Three progression layers

All of ‘skill tree’, ‘learned spells’, ‘unlocked abilities’, and ‘crafting’ collapse into three layers. This structure also drives the persistence model (§6.3).

<table>
<colgroup>
<col style="width: 16%" />
<col style="width: 20%" />
<col style="width: 38%" />
<col style="width: 25%" />
</colgroup>
<thead>
<tr class="header">
<th><strong>Layer</strong></th>
<th><strong>Persistence</strong></th>
<th><strong>Contents</strong></th>
<th><strong>Where changeable</strong></th>
</tr>
</thead>
<tbody>
<tr class="odd">
<td>Character</td>
<td><p>Permanent</p>
<p>(references only)</p></td>
<td><p>Level (drives the compressed stat band only)</p>
<p>+ unlock ledger: parts / spells / keystones</p></td>
<td>Earned via XP + materials</td>
</tr>
<tr class="even">
<td>Crafting</td>
<td>Permanent items</td>
<td>Guns, staffs, special ammo, consumables</td>
<td>Safe zones only</td>
</tr>
<tr class="odd">
<td>Loadout</td>
<td>Fluid</td>
<td>Equipped subset: 1 weapon + gadgets/spells + keystones</td>
<td><p>Safe zones only;</p>
<p>LOCKED in the open world</p></td>
</tr>
</tbody>
</table>

**Loadout-lock outside safe zones is the keystone rule of the whole economy.** It converts ‘breadth’ from in-fight dominance into *preparation and prediction*: you commit to a build on leaving the safe zone and can be hard-countered for it. Owning more options never means bringing more power into a fight — only that you chose better beforehand. Both classes unlock everything eventually; identity lives entirely in the fluid loadout layer.

### 6.2 Crafting: one slotted-blueprint system, skinned twice

Guns and staffs share a single crafting paradigm. A category blueprint exposes a set of slots; clicking a slot lists options, each with material costs and behavioural effects. Components shape behaviour (recoil, spread, cast speed, mana cost, AoE, projectile character), not raw power tier.

|                  | **Gunslinger (gun)**                           | **Mage (staff)**                                    |
|------------------|------------------------------------------------|-----------------------------------------------------|
| Blueprint        | Per gun category                               | Per staff category                                  |
| Slots            | Muzzle, barrel, scope, trigger, magazine, …    | Core, focus, conduit, …                             |
| Component effect | Recoil, spread, mag size, range, handling feel | Cast speed, mana cost, projectile/AoE, element bias |
| Power rule       | Shapes handling & ceiling, not DPS band        | Shapes behaviour & ceiling, not damage band         |

- **Crafting happens only in safe zones.** Raw materials must be physically hauled to a safe outpost to be crafted or spent.

- **Cheap / free respec.** The loadout layer is fully fluid and free to respec; a global respec/refund is granted on every major balance patch. Permanent identity (what you’ve unlocked) is separated from fluid power (what you’ve equipped), so the team can rebalance aggressively without ever invalidating a character.

### 6.3 Persistence & versioning

The save-data problem (keeping characters valid across updates, and rebalancing without migration pain) is solved by one rule: never store computed stats; store only what a player owns, and derive everything at runtime from versioned tuning tables.

- **Saves hold references, not values.** A crafted gun is stored as its recipe (part IDs), never a stat snapshot. A character stores unlocked node IDs and material counts, never derived HP or DPS. Final stats are computed fresh from the current version’s tables on load.

- **All balance numbers live in versioned tuning tables, separate from save data.** Nerfing a barrel edits one row; every gun using it updates on next load. No character migration is ever needed to rebalance — because no balance number was ever in the character.

- **Schema versioning for structural change only.** Each save carries a schema_version; sequential forward-migrations run when the *shape* of the data changes, not when numbers change.

- **Additive-first, deprecate-never-delete.** Adding content never breaks old saves. Retired IDs stay resolvable and map to a replacement or refund; they are never deleted.

- **Cheap respec is what makes aggressive rebalancing safe.** No build is permanent, so no rebalance ‘invalidates’ a character.

> **The one thing this does not solve — economy.** If a rare material is later made easier to farm, veterans who ground it feel cheated; harder, and new players are walled out. Handle this with source/sink tuning going forward and *never retroactively confiscate*: grandfather whatever players already earned.

## 7. World structure

One contiguous open world, sized to support 100+ concurrent players. Two independent axes overlay it: a radial danger gradient and themed biomes.

### 7.1 Radial danger gradient — safe centre, lethal rim

Danger increases outward from a safe central hub. This orientation is chosen for live expandability (P4): a shipped map grows by extending the dangerous rim outward, which is trivial, rather than by rebuilding a fixed dangerous core.

| **Band**            | **Danger**  | **Materials** | **PvP**          | **Role**                                           |
|---------------------|-------------|---------------|------------------|----------------------------------------------------|
| Central hub         | None (safe) | —             | Off              | Main city: crafting, market, respawn, loadout swap |
| Fringe (T1)         | Low         | Common        | Off / restricted | New-player farming, guaranteed-safe grind          |
| Frontier (T2)       | Medium      | Uncommon      | On               | The contested main band; center of gravity         |
| Deadlands (T3, rim) | High        | Rare          | Full             | Heavy-weapon mats, world bosses; no safe outposts  |

<table>
<colgroup>
<col style="width: 100%" />
</colgroup>
<tbody>
<tr class="odd">
<td><p><strong>Bracket separation is economic, not walled.</strong> Reward scales with danger, so a level-80 farming T1 earns trash and simply won’t. Veterans are pulled to the rim, away from newbies, by incentive rather than by level-locked zones (which would break the seamless world).</p>
<p><strong>Accepted problem — the transition band collision.</strong> Because the rim is the whole outer perimeter, a newbie pushing outward walks <em>toward</em> where veterans farm. Softeners: (1) a steep, clearly-signposted gradient so crossing a tier is a deliberate, informed choice; (2) a <em>convex</em> reward curve so the rim is disproportionately better and mid-tiers are nobody’s optimal farm — a pass-through, not a destination; (3) bracket-appropriate outposts (§7.3).</p></td>
</tr>
</tbody>
</table>

### 7.2 Biomes — Model A (type × grade)

Biomes are a second axis, completely independent of the danger rings. Under Model A, a biome defines material TYPE and the radius defines material GRADE/rarity.

- **Biome = what.** A fire biome drops fire-aligned materials (and themed parts) at every danger tier.

- **Radius = how rare / risky.** The same biome yields higher-grade materials the further out you go. ‘High-tier fire mats’ means ‘the outer part of the fire biome’.

- **Consequence: material taxonomy is 2-dimensional — type (biome) × grade (radius).** Every rare recipe has a geographic answer (‘T3-grade ice-biome mats’ = a specific, contestable place), which drives exploration and creates natural PvP hotspots at each biome’s outer edge.

> **Geography must not lock builds.** Element/type-specific mats are biome-gated, but structural/common mats (staff cores, generic components) are available everywhere. A fire mage needs generic mats too and must be able to operate outside the fire region. Same principle as loadouts: specific gated, common universal.

### 7.3 Outposts, respawn, and travel

- **Multiple safe outposts**

- **Discover-to-unlock.** You may spawn at any outpost you have reached at least once. A level-20 cannot one-click spawn at the rim — they had to survive the trip to unlock it. This is a skill/risk filter, not a stat filter, and preserves the seamless world.

- **Travel:** choose any unlocked outpost to spawn at; thereafter travel is on foot. Mounts/vehicles are available (and imply a speed/chase build axis). No fast-travel while carrying raw materials — the haul risk must not be teleportable.

**OPEN —** Outpost blockading. A safe zone whose only exit can be camped is a soft spawn-camp. Mitigations to specify: brief exit-invulnerability and/or multiple exits per outpost.

**OPEN —** Outposts are currently contestable-but-not-destroyable. Whether deep outposts can be blockaded, captured, or upgraded is deferred (see §11, social/territory).

## 8. Economy, death & PvP loop

### 8.1 The core loop: haul-to-craft

Crafting and spending happen only in safe zones, so raw materials must be hauled home through danger. The vulnerable moment is transporting unspent materials — spent materials (learned spells, crafted guns you keep) are safe. This is an EVE-style ‘bank your gains or risk them’ tension: open-world PvP is largely about contesting farming spots and interdicting haulers.

### 8.2 Death penalty

<table>
<colgroup>
<col style="width: 50%" />
<col style="width: 50%" />
</colgroup>
<thead>
<tr class="header">
<th><strong>Kept on death</strong></th>
<th><strong>Lost on death</strong></th>
</tr>
</thead>
<tbody>
<tr class="odd">
<td><p>Gear &amp; weapons (all crafted items you own)</p>
<p>A tier-scaled percentage of carried materials (insurance)</p></td>
<td>Most carried raw materials (drop to the ground)</td>
</tr>
</tbody>
</table>

- **Insurance scales with danger tier only.** A percentage of carried materials survives death, increasing with the danger of the tier you died in — rewarding risk and encouraging rim pushes. It is *flat across squad size*: insurance does not depend on group size.

> **Why squad size was removed from insurance.** An earlier formula reduced insurance for larger squads. That *punishes the cooperation P3 subsidises everywhere else* — squads already pay for safety by splitting loot four ways and dying less often. Insurance rewards risk (tier), full stop; it does not tax grouping.

- **Respawn cost is the walk back.** A geared player who has already spent their materials loses little on death except position — respawning at a safe outpost and walking back is their primary penalty.

**OPEN —** Respawn timer length, and whether a rim death returns you to a far safe outpost (long walk = meaningful cost), are to specify.

### 8.3 Dropped-material rules

- **Killer-squad exclusive for 30s, then free-for-all.** Dropped materials are lootable only by the killing squad for the first 30 seconds (protecting the earner from third-party scavengers), then open to anyone until despawn.

- **Despawn after 15 minutes.** Abandoned drops clear, feeding a scavenger sub-economy in the window between.

- **Picking up is free; crafting with mats is gated.** Anyone can grab dropped mats, but using them in crafting still requires the relevant unlock/blueprint requirements.

> **Accepted harshness.** At 3s TTK on a veteran-heavy rim, a solo hauler is an incentivised target — rim rare mats effectively flow from soloists to gankers. This sharpens ‘cooperation incentivised’ into ‘soloing the rim is economically punished’, consistent with P3. Insurance (tier-scaled) is the blunting knob if this proves too harsh.

### 8.4 Resource sourcing: nodes vs. mobs

Two farming verbs with distinct vulnerability profiles and distinct material roles.

|                    | **Nodes (harvest)**                                       | **Mobs (kill)**                           |
|--------------------|-----------------------------------------------------------|-------------------------------------------|
| Activity           | Static: press-and-wait at a fixed point                   | Active/mobile: you are already fighting   |
| Vulnerability      | Exposed mid-channel, but can react and defend at any time | A fight — you can respond directly        |
| Materials          | Basic / general / structural (universal)                  | Rare / specific / type-aligned            |
| Difficulty scaling | Better mats take longer to harvest                        | Stronger mobs carry rarer loot            |
| PvP flavour        | Fixed flashpoints; fights over locations                  | Interdiction; hunting the exposed/hauling |

- **Node harvest is interruptible by reaction, not committed.** You hold position and press an interact button; you may break off and defend at any moment. Harvest time scales with material grade, so deep-biome harvesting is a real, guarded risk — which feeds the ‘someone mines, someone watches’ squad role (P3).

- **Both verbs are necessary.** You cannot get everything by only fighting or only mining; a farming trip naturally mixes both.

## 9. Squads & world bosses

### 9.1 Squads

- **Size cap: 4.** Small enough to stay coordinated, large enough to cover roles (DPS / control / support / guard).

- **Killing credit = most damage dealt, game-wide.** One consistent credit model across PvP and bosses. (Last-hit is not used — it is gameable and invites kill-stealing within a squad.)

- **Intra-squad loot sharing is a safe-zone-set rule, not a live handoff.** A squad chooses its culture in the safe zone: free-for-all (drops to ground, squad-priority 30s) or shared (mats auto-split among nearby squadmates). One player manually hoarding and redistributing is possible but never the default, because default-hoarding breeds friction.

> **The 30s exclusivity is squad-versus-outsiders, not player-versus-teammate.** It protects the earning squad from third-party scavengers; it is never a mechanism for one squadmate to lock loot away from another.

### 9.2 World bosses

- **Contested and open-world.** Multiple squads fight the boss and, implicitly, each other. Bosses are biome-themed and scale up with the danger of the region they inhabit; rim bosses are the primary heavy-material fountain.

- **Credit is squad-pooled.** A squad’s combined contribution counts as one entry in the ranking. This is the decisive choice: a personal-damage meter would pit squadmates against each other for loot slots and silently punish support builds. Squad-pooled credit makes the boss a contest *between squads (and soloists)* — the intended flashpoint — and lets support mages share their squad’s ranking.

**Loot distribution — two tiers**

| **Tier**                    | **Who**                                   | **How**                                         |
|-----------------------------|-------------------------------------------|-------------------------------------------------|
| Rare / signature loot       | Top 4 contributors (squads or soloists)   | Split by contribution ratio                     |
| Common / participation loot | Everyone above the contribution threshold | Base mats, XP, currency — a participation floor |

- **Participation threshold = a percentage of boss health, not a flat number.** Set as ‘≥ X% of the boss’s HP in contribution’ so it scales with the boss and cannot be cheesed by a single tap. X is to tune; the *form* (percent-of-HP) is fixed.

> **Accepted consequence.** Coordinated 4-stacks will reliably take the top-4 rare slots, so rare boss mats are effectively squad-gated (consistent with P3). The participation floor ensures a soloist who contributed meaningfully still leaves with *something*, so losing the ranking is not a total loss.

## 10. Cross-cutting invariants

Rules that recur across systems and must hold everywhere. A change that breaks one of these breaks a pillar.

- **‘Stronger’ always means higher skill ceiling / commitment at the same power band — never a bigger number.** Applies to gun weight classes, rare-material weapons, staff components, and spell tiers alike.

- **Access is gated; power is not.** Element affinity, part unlocks, and material rarity gate which tools you can reach, never how strong they are.

- **Specific mats are gated; common mats are universal.** Applies to both loadout requirements and geography — no build is locked to one biome.

- **Breadth is a safe-zone advantage only.** Loadout-lock outside safe zones means owning more options is preparation, never in-fight power.

- **Every damaging tool has a dodge vector and a punish window.** Bullets are dodgeable projectiles (snipers excepted within range); spells are telegraphed. Reload/recoil and mana/cooldown are symmetric downtime.

- **Store references, derive from versioned tables.** No computed stat ever lives in save data.

- **Never retroactively confiscate.** Grandfather earned progress across all economy changes.

- **Incentives reward cooperation; nothing taxes it.** Reward and difficulty nudge toward grouping; no rule (e.g. insurance) penalises group size.

## 11. Open questions & deferred scope

Collected known-unresolved items. None block the current design; each is either a refinement or a category deliberately postponed.

### 11.1 Open design decisions

| **Area**           | **Question**                                           | **Recommendation / note**                                |
|--------------------|--------------------------------------------------------|----------------------------------------------------------|
| Ranged-poke role   | No element owns pure ranged poke — gap or intentional? | Decide before finalising the 5-element set               |
| Outpost blockading | How to prevent camping an outpost’s only exit          | Exit-invulnerability and/or multiple exits               |
| Respawn cost       | Respawn timer; does a rim death send you far back?     | Long walk-back = primary geared-player penalty           |
| Mob behaviour      | Leash, aggro range, kiteable onto other players?       | At 3s TTK, mob aggro is a PvP tool — design deliberately |
| Starter kit        | What a zero-material new player spawns with            | Defines the floor of the compressed power band           |

### 11.2 Critical tuning problem — progression pacing

With the handling gate removed, *material grind is the only thing between a new player and every weapon*, so Gunslinger (and to a lesser extent Mage) progression pace is entirely a drop-rate/economy problem with no other lever. The document sets the *form*; values are to tune.

**OPEN —** Set target pacing: rough time to a first real crafted build, and to rim-viability (e.g. ‘first real build in ~X hours, rim-capable in ~Y’). Even coarse targets shape every drop rate.

### 11.3 Deferred by choice

Categories intentionally out of scope for this draft. Each will get its own section later; none affects the systems above.

- **Netcode & server architecture.** A persistent 100+ player shared world is a serious interest-management / tick-rate / area-of-interest problem. Note: the concentric danger structure is a natural partitioning structure — rim (few high-skill players) can afford higher fidelity; fringe (many low-stakes players) can be culled aggressively. Design the map’s danger structure and its server partitioning together when this is picked up.

- **Monetisation & cosmetics.** Must stay non-vertical to preserve P1/P2 (cosmetic and convenience only).

- **Marketplace & player trading.** Interacts heavily with the economy and material grades; a major system in its own right.

- **Social systems / guilds / territory.** Includes whether outposts become capturable — connects to §7.3 open items.

- **Onboarding & tutorial.** Especially important given the Gunslinger’s high mechanical floor and the mage-feels-strong perception (§3).

- **Full numeric balance pass.** HP/damage bands, mana pools, cooldowns, drop rates, insurance %, harvest times, threshold %, XP curves — the spreadsheet that follows this document.

*End of draft — SpellFire GDD v0.1*