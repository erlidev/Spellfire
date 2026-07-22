# Design pillars

These commitments are ordered. When two desirable features conflict, the earlier pillar wins.

## P1 — Skill can always overturn gear

A level-20 player can defeat a level-80 player through aim, movement, prediction, positioning, or the right tactical answer. Gear decides the fight between comparable players, so overturning it takes a clear skill gap and not merely a good day — but the win must stay reachable, and no amount of gear may make a player unkillable, unreactable, or immune to being outplayed. This is SpellFire's identity and its hardest constraint.

## P2 — Vertical progression is real and bounded

Better and rarer gear is substantially stronger, and the total gain is capped by an explicit [vertical budget](progression-and-crafting.md#the-vertical-budget): a fully geared character is about **2× the effective combat power** of one in starter equipment, never more. The gap is meant to be felt — it is the reward the grind is for — and the cap is what keeps it a gap rather than a wall. Progression grants weapons, spells, builds, and counters *and* a real numeric edge; it never grants an unanswerable one.

Because ×2 is decisive, the [danger bands](world.md#radial-danger) carry the fairness load: the players holding it belong on the rim, and the Fringe restricts PvP. Balance protects the newcomer by geography, not by flattening the reward.

Two thirds of the budget is reachable by [rim viability](progression-and-crafting.md#progression-pacing). Past that, additional time buys breadth and skill ceiling, so the veteran-versus-established-player gap stays much smaller than the veteran-versus-newcomer gap.

## P3 — Cooperation is the intended strategy

Squads outperform soloists in the most valuable areas. Exceptional players may solo dangerous content, but grouping remains optimal. Difficulty, rewards, and the economy should encourage cooperation without taxing it.

## P4 — The world is seamless and expandable

SpellFire has one contiguous world, not instanced zones. It must grow without rebuilding existing content; danger therefore rises outward from a safe center. See [`world.md`](world.md).

## Resolved tension: vertical gain without power creep

“Stronger” means greater choice, commitment, or skill ceiling **plus** a bounded numeric gain drawn from the vertical budget. Every increment is spent from a fixed pool rather than added to a running total, so the ceiling does not move when content is added: a new rarest item competes with the existing rarest item, it does not out-scale it. Rarity is at least half horizontal — a rarer item must change conditions, handling, or behavior, not only its numbers.

This interpretation governs every weapon, spell tier, crafted component, and unlock; [`invariants.md`](invariants.md) records the resulting rules.

