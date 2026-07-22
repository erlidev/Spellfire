# Visual and art direction

SpellFire uses a clean, procedural visual language inspired by Diep.io and extended for classes, elements, biomes, danger, and dense multiplayer combat. System shapes are locked here; exact color, size, and density values remain tunable. Renderer choice belongs to [`../../architecture.md`](../../architecture.md).

## Visual pillars

These are ordered; earlier rules win conflicts.

1. **Readability before beauty.** At three-second raw TTK, a crowded fight must be parsed instantly.
2. **Procedural first, authored by exception.** In-world appearance comes from code, parameters, and math, keeping content coherent and cheap to extend.
3. **Form encodes function.** Hue, silhouette, projectile shape, and ground shape communicate gameplay.
4. **Atmosphere comes from palette, not detail.** Mood uses color, value, and grid rather than clutter.

## Primitive vocabulary

The play space uses filled circles, polygons, rectangles/capsules, arcs, and line segments. Interiors are flat fills; optional shading is another flat shape. Gradients are reserved for deliberate effects such as glow or telegraph falloff, never surface texture.

Every entity and projectile has one darker outline of consistent relative weight. The outline preserves overlap clarity and carries allegiance; it is not fixed black.

A thin procedural ground grid supplies motion reference. Its color and cell size are palette-driven.

## Procedural boundary

Everything in-world is generated from primitives: actors, weapons, projectiles, effects, telegraphs, nodes, drops, props, terrain, and grid. Painted sprites and bitmap textures never enter the play space.

Only these outside-the-world assets may be authored:

| Asset | Surface | Reason |
|---|---|---|
| UI icons | HUD, menus, inventory, crafting | Tiny symbolic clarity |
| Logo/wordmark | Title, loading, branding | Brand craft |
| Marketing/splash art | Stores and promotion | Never rendered in play |

When in-world art needs more range, extend the primitive vocabulary or parameters instead of adding a sprite.

## Palette

One base look shifts regionally through three ambient channels:

| Channel | Biome effect | Danger effect |
|---|---|---|
| Background value/tint | Hue by biome | Darkens toward rim |
| Grid color/opacity | Matches biome | Fades/cools toward rim |
| Ambient saturation | — | Desaturates toward rim |

The center is bright and clean; the Deadlands are dark, cold, and desaturated. Ambient state lets players estimate depth without UI.

Element hues stay consistent across spells, telegraphs, materials, and props:

| Element | Reserved hue |
|---|---|
| Fire | Red-orange |
| Frost | Cyan/pale blue |
| Storm / Lightning | Yellow/gold |
| Arcane | Magenta/violet |
| Earth / Stone | Tan/earth-brown |

Exact values require tuning and accessibility validation. The color budget is intentionally split: **fill describes identity**, while **outline and overhead ring describe allegiance/threat**. Backgrounds stay low-saturation. Never color an enemy fill merely because it is hostile.

## Entity language

Players are recognizable bodies holding separate weapons, not bodies fused with weapons.

- Gunslingers use angular, neutral bodies. Their procedural gun is always visible and reveals parts, weight, likely range, and aim.
- Mages use softer, rounder bodies colored by dominant element. Their component-built staff shares that color and remains visible.
- Guns and staffs rotate around a hand pivot so aim direction is always readable.
- Gunslinger deployables and situational heavy swaps appear only when used, preserving the [visibility asymmetry](classes.md#build-visibility).
- Crafted recipes directly drive weapon silhouettes, joining gameplay data and appearance.
- **Rarity is visible on the weapon, never on the body.** Because gear now carries a bounded [power step](progression-and-crafting.md#the-vertical-budget), a player deciding whether to engage must be able to read roughly what they are engaging. Tier reads through a non-hue channel on the gun or staff — added segments, a heavier accent, an inlay count — so it survives the palette and stays subordinate to the element color the staff already carries.

Mobs may use fused body/attachment silhouettes to distinguish them from players. Nodes form material-colored clusters, drops use the material color, and bosses are large biome-themed constructs with distant silhouette clarity.

## Readability system

| Channel | Meaning |
|---|---|
| Fill | Element/entity identity or material type |
| Outline | Self, squad, neutral, or hostile allegiance |
| Overhead ring | Threat, target, or squad marker; redundant with outline |
| Shape/silhouette | Actor class and projectile type |
| Weapon detail | Crafted item rarity tier |
| Opacity | Pending, active, or resolved telegraph state |

Spell telegraphs use a shared translucent circle, cone, line, or ring in the element hue. The shape names the area; hue names the element; fill/intensity shows time to impact; resolution flashes. Players learn this grammar once.

Bullets, sniper projectiles, spell bolts, area seeds, and gadgets need distinct silhouettes. Hits briefly flash the struck body; health and mana use outlined flat bars; death bursts the body's own primitives.

Every hue-coded distinction must also use shape, pattern, outline, ring, icon, or text. Colorblind safety is a requirement, not polish.

## World rendering

The grid remains the ground everywhere. Sparse procedural scatter and ambient tint identify biomes without textures. Safe zones use brighter, calmer ambient and a distinct grid so the loadout-lock boundary is unmistakable. Danger-band transitions use the ambient value ramp defined above.

## Effects and motion

Explosions, muzzle flashes, and impacts use short-lived rings and shape bursts. Glow and bloom are rare emphasis, never haze. Interpolation, easing, recoil/scale pops, and restrained camera feedback carry most polish. Motion cannot obscure telegraphs or hitboxes, and drawn geometry must closely match collision.

The rules assume fast per-frame primitive drawing, independent of renderer. Effect density and glow are tunable to performance.

## Open values

The primitive grammar, outline, procedural boundary, palette model, hue set, channel split, and telegraph system are locked. Exact colors, saturation curves, silhouettes, grid dimensions, and effect density remain open.

**Open:** the colorblind-validated palette. Hues are chosen and validated iteratively against real fights rather than picked up front, and the art style itself may shift as the game finds its look. The requirement that survives any style change is the redundant non-color channel above: no distinction may depend on hue alone, so a palette revision can never be the thing that breaks readability.

