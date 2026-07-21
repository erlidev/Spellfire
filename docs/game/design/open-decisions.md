# Open decisions and deferred scope

These items are unresolved by design, not omissions. Resolve each in its owning topic document, then remove it here.

## Design decisions

| Area | Question | Direction |
|---|---|---|
| Mage range | Should an element own pure ranged poke? | Decide before finalizing the element set |
| Outposts | How is exit camping prevented? | Exit protection, multiple exits, or both |
| Respawn | Timer and rim walk-back cost? | Position loss is the main geared-player penalty |
| Sentry AI | Aggro, leash/reset, speed, cadence, turret tiers? | Behavior shape is locked; tune values |
| Starter kit | What does a zero-material player receive? | Defines the compressed power floor |
| Palette | Which exact hues pass colorblind validation? | Separate by value and shape, not hue alone |
| Rendering | Which 2D backend meets 100-player density? | Architecture owns the choice and effect ceiling |

## Critical tuning problem: progression pacing

Material acquisition is the only gate between a new Gunslinger and every weapon, and a major Mage gate too. Set rough targets for the first coherent crafted build and for rim viability before tuning drop rates. The system shape is fixed; its pace is not.

## Deferred systems

- Netcode and server architecture beyond the implemented foundation. The radial world may inform area-of-interest partitioning.
- Monetization and cosmetics, restricted to non-vertical cosmetic or convenience value.
- Marketplace and player trading.
- Guilds, territory, and possible outpost capture.
- Onboarding and tutorials, especially for the Gunslinger's high floor and low-skill Mage perception.
- Mob classes beyond Sentries, each with its own aggro and combat contract.
- Full numeric balance: stats, resources, cooldowns, drops, insurance, harvest times, boss thresholds, and XP curves.

Player-facing unresolved choices are tracked separately in [`../ui/open-decisions.md`](../ui/open-decisions.md).
