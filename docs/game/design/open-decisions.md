# Open decisions and deferred scope

These items are unresolved by design, not omissions. Resolve each in its owning topic document, then remove it here.

## Design decisions

| Area | Question | Direction |
|---|---|---|
| Palette | Which exact hues pass colorblind validation? | Separate by value and shape, not hue alone. Deferred: the art style may still shift, and hues are validated against real fights rather than chosen up front. See [`visual-direction.md`](visual-direction.md#open-values) |

Recently resolved, now owned by their topic documents: outpost exit camping and player influence ([`world.md`](world.md#outpost-safety)), respawn timer, destinations, and cost ([`economy-death-and-pve.md`](economy-death-and-pve.md#respawn)), Sentry values and variants ([`economy-death-and-pve.md`](economy-death-and-pve.md#sentry)), Mage ranged poke ([`mage.md`](mage.md)), starter kit and progression pacing ([`progression-and-crafting.md`](progression-and-crafting.md#starter-kit)), and renderer choice ([`../../architecture.md`](../../architecture.md#rendering-and-interface)).

## Deferred systems

- Netcode and server architecture beyond the implemented foundation. The radial world may inform area-of-interest partitioning.
- Monetization and cosmetics, restricted to cosmetic or convenience value. Nothing sold may draw on the [vertical budget](progression-and-crafting.md#the-vertical-budget); the budget is earned from materials only.
- Marketplace and player trading.
- Guilds, territory, and possible outpost capture.
- Onboarding and tutorials, especially for the Gunslinger's high floor and low-skill Mage perception.
- Mob classes beyond Sentries, each with its own aggro and combat contract.
- Full numeric balance: stats, resources, cooldowns, drops, insurance, harvest times, boss thresholds, and XP curves.

Player-facing unresolved choices are tracked separately in [`../ui/open-decisions.md`](../ui/open-decisions.md).
