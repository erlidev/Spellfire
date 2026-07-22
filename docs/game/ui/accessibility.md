# Accessibility and usability

Accessibility is part of the interaction contract:

- full keyboard navigation for Home, authentication, and menus;
- visible focus, semantic names, logical reading order, and announced validation/status;
- remappable gameplay controls where the platform permits;
- reliably sized and spaced touch targets;
- text and UI contrast over every biome/danger palette;
- UI and text scaling without loss of critical state;
- reduced motion and camera shake;
- independent audio controls and non-audio critical cues;
- shape, outline, pattern, icon, or text reinforcement for every hue-coded state;
- no essential information available only on hover.

Mobile uses a fixed device-scale viewport so gameplay cannot become stranded in an accidentally zoomed state. Pinch, double-tap, focus, and Safari gesture zoom are disabled; the Home accessibility control remains the supported interface scaling path and must preserve every critical control throughout its range.

**Open:** final palette, live-combat screen-reader scope, controller support, high-contrast mode, and aim/motor assistance. These need dedicated accessibility and competitive-integrity testing.

The in-world channel contract is defined in [`../design/visual-direction.md#readability-system`](../design/visual-direction.md#readability-system); responsive touch behavior lives in [`responsive-and-mobile.md`](responsive-and-mobile.md).
