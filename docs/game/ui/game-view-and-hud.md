# Game view and HUD

The world fills the viewport. Interface chrome protects combat readability and surfaces only state that changes the player's next decision.

## Camera

The local player stays at or near center during ordinary movement. Exceptions include edge clamping, sniper scope, aim look-ahead, scripted transitions, and accessibility settings.

Camera behavior must:

- keep the local player recoverable in crowds;
- avoid motion that undermines aim or telegraph reading;
- keep world indicators registered to entities;
- enforce the Gunslinger's [limited sniper scope](../design/gunslinger.md#snipers);
- account for UI safe areas and touch controls when framing threats.

## Actor labels

Visible players have a compact world-space plate with display name, health, and relevant squad/threat marker. The local plate may be stronger but cannot imply invulnerability. Enemy Mage mana stays private unless game design later makes it public.

Plates simplify with zoom and density. Names may truncate if an accessible full-name path remains. Occlusion priority is: local player, current target/direct threat, squadmates, hostile players, neutral actors.

Fill identifies class/element; outline and overhead ring identify allegiance. Color is never the sole channel.

## Persistent HUD

| Region | Required state | Behavior |
|---|---|---|
| Lower center | Weapon/abilities, slots, cooldowns, charges, class resource, bindings, universal dash | Mage shows mana; Gunslinger shows magazine/reload |
| Lower center/side | Health and statuses | More precise than the actor plate |
| Upper corner | Menu; material connection/latency warning | Always reachable |
| Upper/side | Squad roster, health, state, direction/distance | Conditional, up to four |
| Side | Objective, boss contribution, or interaction tracker | Conditional, collapsible; one dominant panel |
| Lower/side | Carried materials and danger/insurance consequence | Stronger outside safety |
| Edge/corner | Location, biome, danger band, safe-zone state | Map/compass remains Open |

Modules may collapse when irrelevant. Local health, critical combat resources, and menu/exit access never disappear solely because space is tight.

## Contextual prompts and feedback

Prompts appear near their object or in one consistent interaction area. They cover harvesting and interruption, loot priority, safe-zone transitions, services, future vehicles, and actionable failures such as locked loadouts or invalid targets. Show action and input briefly; teach detail elsewhere.

Combat feedback covers damage, healing/shields, status, unavailable cooldown/mana, reload, and death using the design's [telegraph and primitive grammar](../design/visual-direction.md#readability-system). Floating numbers are **Open**; if adopted, they are suppressible and cannot cover telegraphs.

## Safety and danger

- Show current danger and PvP state without opening a menu.
- Announce boundary crossings briefly, then persist the new state.
- Warn before leaving safety locks the loadout.
- Explain material loss/insurance when carrying value into higher danger.
- Show when safe-zone crafting, loadout, and travel services become available.
- Explain travel restrictions at the disabled control.
- Show the [outpost no-PvP radius](../design/world.md#outpost-safety) as a visible boundary, not an invisible rule, so both a player retreating to it and a player chasing them read the same line.
- Show exit invulnerability as a persistent self state with its remaining duration, and state that acting hostilely ends it. It must be legible to attackers too, so a protected player is never mistaken for a valid target.

Warnings teach once and then condense. Reduced warnings must retain persistent danger and PvP state.

