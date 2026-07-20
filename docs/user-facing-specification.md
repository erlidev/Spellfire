# User-Facing Experience Specification

*Status: initial product and interface specification*

This document defines the player-visible structure of SpellFire. It translates the systems in [`gdd.md`](./gdd.md) into screens, controls, feedback, and information hierarchy without prescribing a rendering framework or final visual measurements.

The game follows the familiar interaction model of classic browser `.io` games: a low-friction home page, immediate entry into play, a camera centered on the local player, and a compact in-game interface that preserves as much of the play space as possible. Where this document and the GDD do not specify a behavior, use an established `.io` convention when it is accessible and consistent with SpellFire's design pillars; otherwise record the decision as open rather than inventing a new system.

## 1. Status language and extension rules

Requirements use the following labels:

- **Required** — part of the initial user-facing contract.
- **Conditional** — required when the related GDD system is implemented or relevant to the player.
- **Open** — the need or location is known, but its detailed behavior or values are not yet decided.

New game features should extend the smallest relevant surface in this document. Prefer adding a panel, HUD module, menu tab, or contextual prompt over creating a new top-level screen. Every addition must state:

- when it appears and disappears;
- what information and actions it exposes;
- whether it blocks movement or combat input;
- how it behaves on narrow and touch screens;
- its keyboard, pointer, controller, and touch interaction where applicable;
- its empty, loading, unavailable, error, and reconnecting states;
- which GDD rule it represents.

Exact dimensions, colors, breakpoints, copy, key bindings, and balance values remain in design tokens, tuning data, or future implementation specifications. They should not be hard-coded into this product-level document.

## 2. Experience principles

1. **Enter quickly.** A returning player should be able to start from the home page with one primary action after the necessary character and session state is available.
2. **Keep context.** Account creation, sign-in, settings, and lightweight supporting information open as modals, drawers, popovers, or floating panels. They must not redirect away from the home page or discard a pending play choice.
3. **Protect the play space.** During play, the world and combat telegraphs take priority. Persistent HUD elements sit near screen edges or actors and avoid covering the local player, aim direction, or likely threats.
4. **Show actionable information.** Surface state that affects a player's next decision—health, mana, cooldowns, carried materials, safety, danger, squad state, and interaction progress. Put detailed reference information behind the in-game menu.
5. **Make rules visible.** Safe-zone boundaries, danger transitions, loadout locks, loot eligibility, and death consequences need clear visual feedback rather than relying on prior knowledge.
6. **Preserve skill and readability.** UI must not hide telegraphs, imply inaccurate hitboxes, auto-select tactical answers, or give information the player should not possess. It follows the GDD's visual channel separation and colorblind requirements.
7. **Work at every supported size.** Desktop and mobile present the same essential game state and actions, with layouts and input affordances adapted to available space.

## 3. Application-level flow

The application has three primary states:

1. **Home** — branding, session/account controls, character context, and the central enter-game control.
2. **Connecting/loading** — an in-place transition that reports connection and world-loading status and offers recovery from failure.
3. **In game** — the world view, combat HUD, contextual interfaces, and a menu that can return the player to the home state.

Authentication updates the home page in place. Entering or exiting the game may change application state or route for technical reasons, but should feel like a continuous application rather than a navigation to an unrelated website. Refresh and reconnect behavior must avoid duplicating sessions or silently losing confirmed account or character changes.

## 4. Home page

### 4.1 Layout

The home page uses a full-viewport layout with three conceptual layers:

- **Background:** a lightweight, non-interactive presentation of SpellFire's procedural visual language. It must not reduce text contrast or compete with controls.
- **Utility layer:** account/session access and secondary links aligned to screen edges.
- **Primary play layer:** a compact panel centered in the viewport containing the title/wordmark, current character context, required play choices, and the primary enter-game action.

The centered panel is the strongest visual focus. Promotional content, news, patch notes, legal links, and social links are secondary and must not displace or visually overpower it.

### 4.2 Central play panel

The panel contains, in order:

- SpellFire logo or wordmark;
- service status or blocking maintenance notice, when applicable;
- signed-in identity or guest state;
- character selection/creation entry point when multiple characters or classes are available;
- the selected character's name and class (Gunslinger or Mage);
- the primary **Play** action;
- concise validation or connection errors adjacent to the affected control.

**Required:** Play is disabled only when a blocking requirement is unresolved, and the interface explains that requirement. Activating Play transitions in place to connecting/loading feedback and prevents duplicate connection attempts.

**Open:** Guest play, character-slot count, display-name rules, server/region selection, and whether the first class choice occurs on the home page or in first-time onboarding. Until decided, the design must reserve space for one compact prerequisite control without assuming all of these systems exist.

### 4.3 Account and authentication

Account actions live in a top-corner utility control or similarly unobtrusive floating area. All authentication flows open over the home page in a modal, popover, or narrow-screen sheet and do not perform a full-page redirect for normal operation.

The account surface must support the flows the selected authentication system provides, including as applicable:

- sign in;
- account creation;
- sign out;
- password recovery;
- email or identity verification;
- session-expired reauthentication;
- account/profile settings.

Modal behavior is consistent across flows:

- opening it preserves the home page and selected character state;
- focus moves into it and remains trapped until it closes;
- Escape and an explicit close control dismiss non-blocking dialogs;
- closing restores focus to the invoking control;
- submitting shows an in-context pending state and prevents duplicate submissions;
- field-level errors appear by their fields, while service errors appear at form level;
- successful authentication closes or advances the modal and updates the home page without a redirect;
- destructive account actions require explicit confirmation and are not mixed with routine sign-in actions.

Third-party identity providers may require an external provider window or redirect as an implementation constraint. When used, the home page must preserve pending state and return the player to the same context after completion or cancellation.

### 4.4 Secondary home surfaces

Settings, credits, privacy/legal information, accessibility controls, and patch/news information use secondary modals or drawers where practical. Long-form legal content may open as a dedicated document, but must not be part of the normal play or authentication path.

## 5. Connecting, loading, and recovery

The transition into play replaces or overlays the central play panel with:

- a clear connection/loading state;
- short status text for materially different stages;
- a cancel or return action when cancellation is safe;
- actionable failure messaging and a retry action;
- reconnect behavior after an interrupted active session.

Loading presentation must not simulate progress with misleading precision. If the client needs an asset or world-state download, show determinate progress only when the measurement is real.

On an unexpected disconnect, preserve the last rendered game state only as a visibly inactive backdrop, block gameplay inputs, and show a reconnecting overlay. If recovery fails, return safely to Home with a reason. The exact reconnection grace period is **Open**.

## 6. In-game view and camera

### 6.1 World view

The world occupies the full viewport. The local player remains at or near the screen center during ordinary movement, with camera exceptions allowed for readable edge clamping, scoped weapons, intentional aim look-ahead, scripted transitions, and accessibility settings.

Camera behavior must:

- keep the local player easy to reacquire in crowded encounters;
- avoid abrupt motion that undermines aim or telegraph reading;
- keep world-space indicators registered to their entities;
- respect the sniper's limited scope view and blacked-out peripheral area defined by GDD §4.2;
- ensure UI safe areas and mobile touch controls do not make required threats or interactions unreadable.

### 6.2 Actor labels and bars

Each visible player has a compact world-space plate above the actor containing:

- display name;
- current health bar;
- squad/threat indicator when relevant.

The local player's plate may be slightly emphasized, but cannot be confused with invulnerability or targeting. Mage mana belongs in the local resource HUD rather than above every mage unless a future combat rule makes enemy mana public.

Plates scale or simplify with camera zoom and crowd density. Names may truncate but must expose an accessible full-name path. Occlusion and stacking rules prioritize, in order: the local player, current target or direct threat, squadmates, hostile players, neutral actors. Health changes should be readable without turning every plate into a detailed numeric panel.

Allegiance follows the GDD's channel rules: fill color identifies entity/element, while outline plus an overhead ring identifies self, squad, neutral, or hostile. Color is never the only differentiator.

### 6.3 Persistent HUD

The default HUD uses edge-anchored modules and a clear center:

| Region | Required information | Notes |
|---|---|---|
| Lower center | Equipped weapon or active abilities, slots, cooldowns, charges, reload, and key/touch bindings | Mage presentation also shows mana; Gunslinger presentation shows magazine/reload state. The universal dash is always represented. |
| Near lower center or lower side | Local health and status effects | More precise than the world-space health bar; must remain glanceable during combat. |
| Upper corner | Compact menu button and connection/latency warning when material | Must remain reachable on mobile. |
| Upper/side region | Squad roster with member health, state, and direction/distance when known | **Conditional** on being in a squad; supports up to four members. |
| Side region | Contextual objective, boss contribution, or interaction tracker | **Conditional** and collapsible; only one dominant activity panel should claim this region at a time. |
| Lower/side region | Carried material summary and danger/insurance consequence | Emphasized outside safe zones because hauling is the central risk loop. |
| World edge/compact corner | Location, biome, danger band, and safe-zone state | Exact minimap/compass implementation is **Open**. Do not promise information beyond player knowledge. |

HUD modules may collapse when their state is irrelevant. Critical combat resources, local health, and the menu/exit path must never disappear solely because the viewport is small.

### 6.4 Contextual prompts and feedback

Context-sensitive prompts appear near the relevant actor/object or in a consistent interaction area. They cover:

- harvesting a node, including progress and interruption;
- picking up dropped materials, including squad-exclusive availability;
- entering or leaving a safe zone;
- interacting with crafting, market, loadout, respawn, or outpost services;
- mounting or using a vehicle when implemented;
- errors such as a locked loadout, missing materials, unavailable loot, or invalid target.

Prompts show the available action and input, not a paragraph of explanation. Longer explanations belong in the menu or a first-time contextual teaching surface.

Combat feedback includes damage, healing/shielding, status effects, cooldown/mana failures, reload state, and death. It must use the primitive-based feedback and standardized telegraph grammar in GDD §10.6–10.8. Floating numbers are **Open**; if adopted, they must be suppressible and must not obscure telegraphs.

### 6.5 Safety and danger communication

The interface reinforces the world's radial danger model:

- current danger band and PvP state are visible without opening a menu;
- crossing a safety or danger boundary produces a brief, non-blocking notice and a persistent state change;
- leaving a safe zone clearly warns that the loadout will lock before the crossing becomes final;
- carrying materials into higher danger communicates the applicable death risk/insurance in plain language;
- safe-zone services visually indicate when crafting, loadout changes, and spawning/travel choices become available;
- no-fast-travel-while-carrying restrictions explain themselves at the travel control.

Warnings should inform once and then become concise; they must not repeatedly interrupt experienced players. Any option to reduce repeated warnings must retain the persistent danger and PvP indicators.

## 7. In-game menu

The menu opens from a persistent button and a conventional keyboard/controller binding. It is an overlay on desktop and may become a full-screen panel on mobile. Opening it does **not** pause the shared world.

The menu must say that the world remains active, preserve awareness of immediate danger where possible, and avoid implying safety. Movement/combat input is suspended while interacting with menu controls unless a specific non-blocking panel explicitly allows it.

### 7.1 Menu information architecture

The menu grows through tabs or equivalent top-level sections:

| Section | Player-facing contents | Availability |
|---|---|---|
| Character | Name, class, level, XP progress, unlock ledger summary | Always |
| Loadout | Equipped weapon, gadgets/spells, keystones, and readable effects; explains why editing is locked outside safe zones | Always; editable only in safe zones |
| Inventory & materials | Owned items, carried raw materials, material type/grade, and death-risk treatment | Always |
| Crafting | Blueprint slots, compatible components, costs, results, and validation | Safe zones only |
| World | Discovered outposts, biome/danger information, and available spawn/travel choices | View always; actions follow GDD restrictions |
| Squad | Members, invitations, leadership controls, and safe-zone loot-sharing rule | Conditional; exact social operations are **Open** |
| Activity | World-boss contribution/rank, participation threshold, and other active contextual progress | Conditional |
| Reference | Concise explanation of combat resources, death, loot priority, danger bands, and controls | Always |
| Settings | Input, audio, graphics, interface, accessibility, and account-safe preferences | Always |

Marketplace/trading, guild/territory, monetization/cosmetics, and full onboarding are deferred in the GDD. If adopted, they receive new sections or safe-zone service panels following the extension rules in §1; they must not be preemptively represented as working features.

### 7.2 Exit and session actions

The menu has a clearly labeled **Exit game** action, separated from settings and destructive account actions. Activating it:

1. explains any known immediate consequence, including whether the character remains vulnerable during a disconnect period;
2. requests confirmation when leaving can cause material or positional risk;
3. prevents accidental activation during normal menu navigation;
4. disconnects cleanly and returns to the Home state without signing the account out.

Sign out is an account action on Home, not a substitute for Exit game. Browser/app closure and network loss should use the same server-authoritative disconnect rules. Logout safety, delay, and combat-logging behavior are **Open** and must be decided before the exit flow is treated as final.

## 8. System-specific user interfaces

### 8.1 Safe-zone loadout and crafting

Loadout and crafting interfaces share component terminology and presentation because guns and staffs use the same slotted-blueprint system.

The crafting flow shows:

- selected blueprint and its slots;
- valid components for the selected slot;
- owned/required materials and material shortfalls;
- behavior changes in plain language, without presenting rare parts as a higher power tier;
- a confirmation summary before spending materials;
- success, inventory-capacity, stale-state, and server-rejection outcomes.

The loadout flow validates slot limits, Mage element-affinity requirements, and other equip rules before commit. On leaving a safe zone, the final equipped set becomes visibly locked. Viewing remains available everywhere.

### 8.2 Death and respawn

Death replaces combat controls with a focused summary that distinguishes:

- gear and weapons kept;
- insured materials kept;
- raw materials dropped;
- death location and eligible respawn outposts;
- respawn availability/timer when defined.

The player selects only discovered, eligible outposts. The screen explains unavailable choices and the walk-back consequence without suggesting that level gates exist. Exact timers and rim-death restrictions remain **Open** per the GDD.

### 8.3 Squads and loot

Squad UI supports a maximum of four members and makes the selected loot rule—free-for-all or shared—visible before the squad leaves a safe zone. Changing that rule is limited to a safe zone. Dropped-material feedback distinguishes squad-exclusive, free-for-all, and despawning states without relying only on color.

World-boss UI reports the squad-pooled contribution model, current contribution feedback where server-authoritative data permits it, rare/signature ranking eligibility, and participation eligibility. It must not present support play as personal underperformance when contribution is pooled at squad level.

## 9. Responsive and mobile behavior

Mobile is a first-class layout, not a scaled-down desktop page.

### 9.1 Home and modal surfaces

- The central play panel remains the first content in reading and focus order.
- Modals become bottom sheets or near-full-screen dialogs when needed, respecting device safe areas and the on-screen keyboard.
- Forms use appropriate input types, visible labels, large touch targets, and error text that remains visible when the keyboard opens.
- Secondary content collapses behind explicit controls rather than pushing Play below several screens of content.
- Landscape and portrait layouts are both supported for Home; the gameplay orientation policy is **Open**.

### 9.2 Gameplay layout and touch input

The gameplay viewport respects notches, rounded corners, browser chrome, and gesture areas. Touch controls occupy configurable lower-side zones while the critical world view remains readable around the centered player.

The initial touch convention is:

- virtual movement control on the lower left;
- aim/primary action control on the lower right;
- abilities, dash, interact, reload or class-equivalent actions within reachable thumb zones;
- menu and low-frequency information controls near upper safe-area edges.

Exact control mechanics—including fixed versus floating sticks, aim assist, target assistance, and orientation—are **Open** because they affect combat balance. They require playtesting against the GDD's skill and dodgeability pillars before becoming requirements.

On small screens:

- squad and activity panels collapse to summaries with explicit expansion;
- labels use collision avoidance and density reduction, but never hide the local player's state or an immediate hostile threat;
- touch controls may fade while inactive but remain discoverable and return immediately on contact;
- important controls cannot rely on hover, right-click, or tiny drag targets;
- the in-game menu may cover the world but must continue to communicate that play is not paused.

## 10. Accessibility and usability

Accessibility is part of the required interaction contract:

- full keyboard navigation for Home, authentication, and menus;
- visible focus, semantic labels, logical reading order, and announced validation/status changes;
- remappable gameplay controls where the platform permits it;
- touch targets sized and spaced for reliable activation;
- text and UI contrast maintained over every biome/danger palette;
- UI scale and text scale controls that do not hide critical state;
- reduced motion and reduced camera shake options;
- independent audio controls and non-audio cues for critical events;
- every hue-coded state reinforced by shape, outline, pattern, icon, or text as required by GDD §10.6;
- no essential information available only on hover.

**Open:** the final element palette, screen-reader scope during live combat, controller support level, high-contrast mode, and detailed aim/motor assistance. These decisions need dedicated accessibility and competitive-integrity testing.

## 11. Shared interface states

Every network-backed surface must define:

- **Loading:** the existing context remains stable while the requested data resolves.
- **Empty:** explains what the area represents and, where appropriate, how to populate it.
- **Unavailable/locked:** states the GDD rule or prerequisite, such as “Loadouts can only be changed in a safe zone.”
- **Error:** describes the failed action in player language and offers retry or recovery when safe.
- **Stale/conflict:** refreshes server-authoritative inventory, crafting, squad, or character state before allowing a contradictory action.
- **Offline/reconnecting:** blocks actions that cannot be confirmed and never pretends a spend, craft, pickup, or loadout change succeeded.

Destructive or spend actions are confirmed once, remain idempotent across retries, and show a server-confirmed result. Routine reversible actions should not accumulate unnecessary confirmation dialogs.

## 12. Deferred decisions

The following user-facing choices are intentionally unresolved and should be added to the GDD or a focused feature specification before implementation is considered final:

- guest accounts, character count, naming rules, and first-time class selection;
- region/server selection and maintenance/service-status behavior;
- first-time onboarding and tutorial flow;
- minimap, compass, map discovery, and permissible information visibility;
- combat floating text and detailed target inspection;
- logout delay, combat logging, disconnect grace, and respawn timing;
- mobile gameplay orientation, touch-control model, and assistance boundaries;
- controller support and live-combat screen-reader scope;
- marketplace/trading, guild/territory, cosmetics/monetization, and related navigation;
- final UI tokens, breakpoints, bindings, copy, icon set, and accessibility-validated palette.

When one of these is decided, replace its **Open** statement in the relevant section with a testable requirement and remove it from this list. The GDD remains authoritative for game rules; this document remains authoritative for how confirmed rules are presented to and operated by the player.
