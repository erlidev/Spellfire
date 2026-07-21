# Responsive and mobile behavior

Mobile is a first-class layout, not scaled desktop.

## Home and overlays

- Keep the central play panel first in reading and focus order.
- Convert modals to bottom sheets or near-full-screen dialogs when needed.
- Respect safe areas and the on-screen keyboard.
- Use appropriate input types, visible labels, large targets, and keyboard-visible errors.
- Collapse secondary content behind explicit controls rather than pushing Play far down the page.
- Support portrait and landscape Home. Gameplay orientation remains **Open**.

## Gameplay and touch

The viewport respects notches, rounded corners, browser chrome, and gesture areas. Controls occupy configurable lower-side regions while threats remain visible around the centered player.

Initial placement:

- movement at lower left;
- aim/primary action at lower right;
- abilities, dash, interact, reload/class equivalent in reachable thumb zones;
- the six [equipped slots](game-view-and-hud.md#slot-selection) as buttons — currently a plain row above the controls, which is functional but not yet placed for one-handed reach;
- menu and low-frequency information near upper safe-area edges.

Fixed versus floating sticks, aim/target assistance, and orientation are **Open** because they affect combat balance. Validate them against the [skill and dodgeability rules](../design/combat.md) through playtesting.

On small screens:

- collapse squad/activity panels to expandable summaries;
- reduce label density without hiding the local player or immediate threats;
- allow controls to fade while idle only if they remain discoverable and return on contact;
- never require hover, right-click, or tiny drag targets;
- let the menu cover the world while still stating that play continues.

