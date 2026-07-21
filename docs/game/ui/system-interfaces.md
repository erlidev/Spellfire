# System-specific interfaces

These surfaces appear when their underlying [game systems](../design/README.md) are available. Shared network and transaction behavior follows [`shared-states.md`](shared-states.md).

## Safe-zone loadout and crafting

Loadout and crafting share terminology and component presentation because guns and staffs use one [slotted-blueprint system](../design/progression-and-crafting.md#slotted-blueprint-crafting).

Crafting shows:

- selected blueprint and slots;
- compatible components for the active slot;
- owned and required materials, including shortfalls;
- behavior changes in plain language without calling rare parts a higher power tier;
- a spend confirmation summary;
- success, capacity, stale-state, and server-rejection outcomes.

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

