# Visual and art direction

SpellFire uses a procedural visual language: a dense, intricately detailed environment beneath a clean, flat combat layer whose readability is never negotiable. Its shape grammar is inspired by Diep.io and extended for classes, elements, biomes, danger, and dense multiplayer combat; its environment aims at the depth a top-down world can carry without a single painted asset. System shapes are locked here; exact color, size, and density values remain tunable. Renderer choice belongs to [`../../architecture.md`](../../architecture.md).

## Visual pillars

These are ordered; earlier rules win conflicts.

1. **Readability before beauty.** At three-second raw TTK, a crowded fight must be parsed instantly.
2. **Procedural first, authored by exception.** In-world appearance comes from code, parameters, and math, keeping content coherent and cheap to extend.
3. **Form encodes function.** Hue, silhouette, projectile shape, and ground shape communicate gameplay.
4. **Detail below, contrast above.** The world is dense and intricate; the gameplay layer that sits on it is flat and loud. Depth is bought in the layers players do not have to parse, never in the layer they do.

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

## The layer model

The world is expansive and intricately detailed, and it stays readable because detail and contrast are separated by depth rather than traded against each other. Five layers, drawn in order:

| Layer | Contents | Technique |
|---|---|---|
| **L0 Ground** | Biome-blended colour, macro variation, flow, cracks, moisture, the grid | One multi-octave noise fragment shader over a single quad |
| **L1 Decals** | Grass tufts, ash drifts, ice fracture, root networks, scorch, rubble | Instanced sprites off runtime-generated shared textures |
| **L2 Terrain** | Colliders: ridges, boulders, ruins, thickets, trunks, walls | Flat fill plus outline, the primitive vocabulary above |
| **L3 Gameplay** | Actors, weapons, projectiles, telegraphs, drops, nodes | Flat fill plus outline, maximum contrast, unchanged |
| **L4 Overhead** | Canopies, overhangs, cloud shadows, ambient particles | Partial alpha, offset against the camera by notional height |

L0 and L1 are where the impression of an intricate world comes from, and they are close to free: a shader over one quad and a few batched draws, regardless of how dense the result looks. This is the whole reason the procedural boundary survives contact with the ambition — density that would be ruinous as thousands of individual shapes is trivial as one noise field and one instanced texture. **Detail is baked into textures and batched; it is never emitted as per-instance geometry.**

L4 implies vertical extent: a canopy slides against its own trunk as the camera passes it, which is the cheapest cue that the world is not a diagram.

Two rules bound the model, and both are checked rather than left to taste.

- **The readability floor.** L0 and L1 are clamped to a bounded value and saturation range, in every biome and every danger band, so L3 always wins contrast against whatever it stands on. A palette or density change that breaches the floor is a defect, not a style.
- **L4 never hides an actor.** Line of sight is authoritative absence — the server omits what a player cannot see. An overhead layer that concealed a body the server *did* send would be a vision rule nobody enforces and no player could read, letting a player be killed by an opponent they could not have seen. Canopy alpha stays low enough that an actor beneath it is always legible.

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

The grid remains the ground everywhere, now carried in L0 alongside the biome's own surface rather than drawn over a flat fill. A biome is identified by its ground field, its scatter, its terrain archetypes, and its ambient tint together — a player should be able to name where they are standing without reading the HUD. Safe zones use brighter, calmer ambient and a distinct grid so the loadout-lock boundary is unmistakable. Danger-band transitions use the ambient value ramp defined above, so depth is estimable without UI.

Scatter density is a per-biome parameter and is expected to be high; the readability floor, not restraint, is what keeps it safe.

## Effects and motion

Explosions, muzzle flashes, and impacts use short-lived rings and shape bursts. Glow and bloom are rare emphasis, never haze. Interpolation, easing, recoil/scale pops, and restrained camera feedback carry most polish. Motion cannot obscure telegraphs or hitboxes, and drawn geometry must closely match collision.

The rules assume fast per-frame primitive drawing, independent of renderer. Effect density and glow are tunable to performance.

## Open values

The primitive grammar, outline, procedural boundary, layer model, readability floor, palette model, hue set, channel split, and telegraph system are locked. Exact colors, saturation curves, silhouettes, grid dimensions, scatter density, and effect density remain open.

**Open:** the colorblind-validated palette. Hues are chosen and validated iteratively against real fights rather than picked up front, and the art style itself may shift as the game finds its look. The requirement that survives any style change is the redundant non-color channel above: no distinction may depend on hue alone, so a palette revision can never be the thing that breaks readability.

