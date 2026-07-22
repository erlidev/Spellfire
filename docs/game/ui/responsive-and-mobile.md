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

- a fixed movement stick at lower left;
- a fixed aim/fire stick at lower right; dragging aims and holds the selected action, while tapping or holding the world still aims and fires directly;
- dash and interact beside movement, with reload and scope/class equivalent beside aim, all in reachable thumb zones;
- the six [equipped slots](game-view-and-hud.md#slot-selection) as buttons — currently a plain row above the controls, which is functional but not yet placed for one-handed reach;
- menu and low-frequency information near upper safe-area edges.

The sticks are fixed rather than floating so their centers and travel remain predictable during multi-touch play. Aim/target assistance and orientation remain **Open** because they affect combat balance. Validate them against the [skill and dodgeability rules](../design/combat.md) through playtesting.

Touch pointers are captured by the control that started them, so movement, aim/fire, and a utility action can be held independently. Essential overlay actions activate on touch pointer release as well as keyboard/mouse click. The gameplay surface disables iOS selection, callouts, and tap highlighting while form fields remain selectable and editable.

On small screens:

- collapse squad/activity panels to expandable summaries;
- reduce label density without hiding the local player or immediate threats;
- allow controls to fade while idle only if they remain discoverable and return on contact;
- never require hover, right-click, or tiny drag targets;
- let the menu cover the world while still stating that play continues.
