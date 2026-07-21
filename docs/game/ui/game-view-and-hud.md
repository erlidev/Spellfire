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

World items use geometry matching their authoritative collision shape. Damaged destructible items show a compact health bar; full-health terrain stays quiet, and items with undestroyable health show no misleading empty bar.

## Persistent HUD

| Region | Required state | Behavior |
|---|---|---|
| Lower center | Weapon/abilities, slots, cooldowns, charges, class resource, bindings, universal dash | Mage shows mana; Gunslinger shows magazine/reload |
| Lower center | Six equipped slots with their bindings and the selected one marked | Selection uses border and weight as well as fill, never color alone |
| Lower center/side | Health and statuses | More precise than the actor plate |
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

The [six equipped slots](../design/progression-and-crafting.md#slots) bind to **1–6**, and the **mouse wheel** steps through them and wraps in both directions. Touch gets six slot buttons; that is the minimum viable placement rather than the settled one, and a touch layout that carries six slots without crowding the aim and movement zones is still owed. The selection travels with every input, so the server resolves the use button against the slot the player actually had selected rather than against whatever arrived last. An empty slot does nothing, visibly: it is a slot the player has not filled, not a failure to report. Bindings are remappable once [remappable controls](accessibility.md) land.

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
