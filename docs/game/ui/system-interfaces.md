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
- death location and eligible outposts;
- timer/availability when defined.

Players may select only discovered eligible outposts. Explain unavailable locations and walk-back cost without implying level gates. Timer and rim restrictions remain **Open** in [game design](../design/economy-death-and-pve.md#death).

## Squads, loot, and bosses

Squad UI supports four players and shows the selected free-for-all/shared rule before leaving safety. The rule changes only in a safe zone. Drop feedback distinguishes squad-exclusive, free-for-all, and despawning states without color alone.

Boss UI explains pooled squad contribution, shows authoritative progress where available, and distinguishes rare-ranking from participation eligibility. It cannot frame support play as personal underperformance when ranking is pooled by squad.

