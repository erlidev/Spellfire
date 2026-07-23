# Game view and HUD

The world fills the viewport. Interface chrome protects combat readability and surfaces only state that changes the player's next decision.

## Camera

The local player stays at or near center during ordinary movement. Exceptions include edge clamping, sniper scope, aim look-ahead, scripted transitions, and accessibility settings.

Camera behavior must:

- keep the local player recoverable in crowds;
- avoid motion that undermines aim or telegraph reading;
- keep world indicators registered to entities;
- enforce the Gunslinger's [limited sniper scope](../design/gunslinger.md#snipers);
- knock briefly with each of the local player's shots, scaled to the weapon's recoil, without ever rotating aim away from the pointer;
- go completely white while the local player is [flashed](../design/gunslinger.md#defense), and clear as the status ends — the server is already sending nothing to draw behind it;
- account for UI safe areas and touch controls when framing threats.

The server interest area covers the complete axis-aligned square whose half-width and half-height equal the configured maximum view distance. Corners of the rectangular camera therefore do not lose entities to a circular range cutoff. Inside that area, terrain marked `occludes_vision` removes actors, projectiles, telegraphs, and fields whose direct sightline it crosses. A half-resolution shadow shader gives those occluded pixels a restrained 27%-opacity dark overlay—not an opaque fog—while entities marked `visible_in_shadow`, including trees, decorations, and walls, remain above it. Trees are landmarks rather than sight blockers. Snapshot omission removes a newly hidden actor immediately; destroying or expiring the blocker reveals what was behind it immediately while its visual fade finishes.

## Actor labels

Visible players have a compact world-space plate with display name, health, and relevant squad/threat marker. The local plate may be stronger but cannot imply invulnerability. Enemy Mage mana stays private unless game design later makes it public.

Plates simplify with zoom and density. Names may truncate if an accessible full-name path remains. Occlusion priority is: local player, current target/direct threat, squadmates, hostile players, neutral actors.

Fill identifies class/element; outline and overhead ring identify allegiance. Color is never the sole channel.

World items use geometry matching their authoritative collision shape. Damaged destructible items show a compact health bar; full-health terrain stays quiet, and items with undestroyable health show no misleading empty bar.

## Persistent HUD

| Region | Required state | Behavior |
|---|---|---|
| Lower center | Weapon/abilities, slots, cooldowns, charges, class resource, bindings, universal dash | Mage shows mana; Gunslinger shows magazine/reload, or Heat while Thermal cycle is equipped |
| Lower center | Six equipped slots with their bindings and the selected one marked | Selection uses border and weight as well as fill, never color alone |
| Lower center/side | Health and statuses | More precise than the actor plate |
| Lower center/side | Shield durability, while one is carried | Reads `broken` at zero; absent for a body holding no shield |
| Upper corner | Menu; material connection/latency warning | Always reachable |
| Upper/side | Squad roster, health, state, direction/distance | Conditional, up to four |
| Side | Objective, boss contribution, or interaction tracker | Conditional, collapsible; one dominant panel |
| Lower/side | Carried materials and danger/insurance consequence | Stronger outside safety |
| Edge/corner | Location, biome, danger band, safe-zone state | Map/compass remains Open |

Modules may collapse when irrelevant. Local health, critical combat resources, and menu/exit access never disappear solely because space is tight.

## Contextual prompts and feedback

Prompts appear near their object or in one consistent interaction area. They cover harvesting and interruption, loot priority, safe-zone transitions, services, future vehicles, and actionable failures such as locked loadouts or invalid targets. Show action and input briefly; teach detail elsewhere.

The initial interaction binding is E on keyboard and Use on touch. The protocol carries the action now; later contextual systems change the prompt and outcome, not the input contract.

## Slot selection

The [six equipped slots](../design/progression-and-crafting.md#slots) bind to **1–6**, and the **mouse wheel** steps through them and wraps in both directions. Touch gets six stable, 44 px minimum slot buttons in a centered row above the utility actions; dash/interact sit above movement and reload/scope above aim so every fixed stick stays centered in its half of the screen. Snapshot-rate cooldown refreshes update these buttons in place rather than replacing them during a press. The slot row remains the minimum viable placement rather than the settled one, and a layout optimized for one-handed reach is still owed. The selection travels with every input, so the server resolves the use button against the slot the player actually had selected rather than against whatever arrived last. An empty slot does nothing, visibly: it is a slot the player has not filled, not a failure to report. Bindings are remappable once [remappable controls](accessibility.md) land.

Firing is legible from outside the shooter as well as inside it: the drawn weapon sits wherever the recoil pattern has walked it, and each shot shoves it back along its own axis with a muzzle flash. Both come from the snapshot, so an opponent watching a spray sees the same walk its owner does — that is what makes a pattern something to play around. The camera is knocked only by a weapon that fires an explosive, so the shot a launcher takes is felt while ordinary gunfire leaves the view still to aim through. A deployed smoke cloud draws over the bodies inside it as drifting volume with a soft, ragged edge rather than a hard bubble, covering the radius the server hides bodies inside — a body or a round the fog does not cover completely is still drawn, and still sent. A Mage's placed ground reads the same way and is tinted by its element: a burning patch, a chilling aura, and a blizzard are drawn as drifting volume in their own colour, and unlike smoke they are drawn *beneath* the bodies standing in them, because the server still sends everything inside them and painting over it would hide what the player is entitled to see. A stone wall is drawn as the destructible rock it is, with the same health bar damaged terrain carries, so both sides can see how much of it is left. A raised shield is drawn as the arc it actually blocks, and that arc thins and shortens as its durability is spent, so an attacker can see a shield running out and decide whether pressing it is worth the ammunition; its owner reads the same pool as a bar beside health. Any slot with a lockout of its own — a gadget's throw, every spell above tier one — shows the time remaining on it, and that readout is the server's own count of what is left, sent to the slot's owner on every snapshot: it starts on exactly the uses that were charged and never on one that was refused.

Combat feedback covers damage, healing/shields, status, unavailable cooldown/mana, reload, and death using the design's [telegraph and primitive grammar](../design/visual-direction.md#readability-system). Floating numbers are **Open**; if adopted, they are suppressible and cannot cover telegraphs.

## Safety and danger

- Show current danger and PvP state without opening a menu.
- Announce boundary crossings briefly, then persist the new state.
- Name the biome and the grade of the ground beside the danger band, because those are the [two axes](../design/world.md#biomes-type--grade) a player is standing on: the biome decides which material the ground can yield and the distance from the hub decides its grade. Announce a biome crossing the way a band crossing is announced, then persist it.
- Colour the ground by the biome, cross-fading across a border rather than switching at a line, so a player can name where they are standing without reading the readout. That tint stays well below the gameplay layer's contrast: it colours the ground, never the actors on it.
- Warn before leaving safety locks the loadout.
- Explain material loss/insurance when carrying value into higher danger.
- Show when safe-zone crafting, loadout, and travel services become available.
- Explain travel restrictions at the disabled control.
- Show the [outpost no-PvP radius](../design/world.md#outpost-safety) as a visible boundary, not an invisible rule, so both a player retreating to it and a player chasing them read the same line.
- Show exit invulnerability as a persistent self state with its remaining duration, and state that acting hostilely ends it. It must be legible to attackers too, so a protected player is never mistaken for a valid target.

Warnings teach once and then condense. Reduced warnings must retain persistent danger and PvP state.
