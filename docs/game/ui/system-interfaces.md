# System-specific interfaces

These surfaces appear when their underlying [game systems](../design/README.md) are available. Shared network and transaction behavior follows [`shared-states.md`](shared-states.md).

## Safe-zone loadout and crafting

Loadout and crafting share item terminology, while guns and staffs get purpose-built views over the same [recipe-blueprint system](../design/progression-and-crafting.md#recipe-blueprint-crafting).

Crafting shows:

- a generic blueprint silhouette with required blank boxes;
- drag-and-drop compatible parts, with click/tap selection as the accessible touch and keyboard equivalent;
- owned and required materials, including shortfalls;
- the resulting weapon preview only when every blank completes one recipe;
- a side list of craftable recipes with concise explanations, plus locked recipes labelled by unlock level;
- behavior changes in plain language, and the part's [rarity tier](../design/progression-and-crafting.md#rarity-tiers) beside them;
- for a completed arrangement, where the result lands on the vertical scale against the character's current weapon — a direct better/worse comparison, not a raw multiplier, since the numbers are bounded and a player should not have to do arithmetic to know whether to spend;
- a spend confirmation summary;
- success, capacity, stale-state, and server-rejection outcomes.

For guns, the recipe list selects the intended pattern but does not decide the result: receiver, barrel, action, feed, and sight must themselves resolve to that gun. For staffs, the same workbench becomes a two-stage assembly list: choose a crafted mana-crystal recipe, then a wood-based stave recipe. Incompatible staves disappear from the compatible choices, and changing a crystal clears a now-under-tier stave rather than allowing a doomed commit.

Both surfaces also list content the character has *not* unlocked, disabled and labelled with the level that grants it. A kit that hides everything still locked reads as finished rather than as a starting point, and a player has no reason to believe an empty gadget slot will ever fill.

Loadout validates slot limits, Mage affinity, and other equip rules before commit. Leaving a safe zone visibly locks the final set; viewing remains available everywhere.

## Death and respawn

Death replaces combat controls with a focused summary of:

- gear and weapons kept;
- insured materials kept;
- raw materials dropped;
- death location;
- respawn destinations;
- the remaining respawn timer.

Respawn is a free choice among every discovered outpost plus the central hub, per [game design](../design/economy-death-and-pve.md#respawn). The nearest outpost to the death location is preselected so the common case — get back to the fight and contest the drop — needs no interaction. Each destination shows its distance from the death location and its danger band, since redeploying to an unfamiliar band is the main way this menu goes wrong.

The list grows over a character's lifetime, so it must stay scannable rather than becoming a wall of names: order by distance from the death location, keep the hub distinctly separated as the reset option, and group or collapse by region once the count is large. Undiscovered outposts are never shown as locked rows — an undiscovered outpost is unknown, not withheld, and rendering it as a locked entry implies a level gate that does not exist.

The five-second timer counts down visibly. Selection is allowed during it, and confirming early does not skip it; the summary stays readable for the full duration so a player is never forced to choose before reading what they lost. If the timer expires without a choice, the preselected nearest outpost is used.

## Squads, loot, and bosses

Squad UI supports four players and shows the selected free-for-all/shared rule before leaving safety. The rule changes only in a safe zone. Drop feedback distinguishes squad-exclusive, free-for-all, and despawning states without color alone.

Boss UI explains pooled squad contribution, shows authoritative progress where available, and distinguishes rare-ranking from participation eligibility. It cannot frame support play as personal underperformance when ranking is pooled by squad.
