# SpellFire user-facing specification

These documents are the source of truth for how confirmed game rules appear to and are operated by players. Game rules belong in [`../design/`](../design/README.md); implemented behavior belongs in [`../../architecture.md`](../../architecture.md).

SpellFire follows a low-friction browser `.io` model: quick entry, a local-player-centered camera, and a compact interface that protects the play space. When neither this specification nor game design resolves a behavior, use an accessible established convention or mark the decision open.

## Status language

- **Required:** part of the initial player contract.
- **Conditional:** required when its game system is implemented or relevant.
- **Open:** the need is known; behavior or values are unresolved.

## Extension contract

Extend the smallest relevant surface—a panel, HUD module, menu section, or prompt—before creating a top-level screen. Every addition defines:

- appearance and dismissal conditions;
- visible information and actions;
- whether it blocks movement or combat;
- narrow-screen behavior;
- keyboard, pointer, controller, and touch interaction where applicable;
- empty, loading, unavailable, error, and reconnecting states;
- the game-design rule it represents.

Exact dimensions, tokens, breakpoints, copy, bindings, and balance values belong in implementation or tuning specifications.

## Specification map

- [`experience-principles.md`](experience-principles.md)
- [`application-flow.md`](application-flow.md)
- [`home.md`](home.md)
- [`connection-and-recovery.md`](connection-and-recovery.md)
- [`game-view-and-hud.md`](game-view-and-hud.md)
- [`game-menu.md`](game-menu.md)
- [`system-interfaces.md`](system-interfaces.md)
- [`responsive-and-mobile.md`](responsive-and-mobile.md)
- [`accessibility.md`](accessibility.md)
- [`shared-states.md`](shared-states.md)
- [`open-decisions.md`](open-decisions.md)

