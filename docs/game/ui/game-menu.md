# In-game menu

The menu opens from a persistent control as a small non-modal panel anchored to the top-right corner. Fine-pointer clients minimize it to a compact title bar shortly after the pointer leaves and restore it on hover; touch clients use the visible minimize button exclusively. Synthetic touch hover never expands or collapses the panel. The shared world **does not pause**.

State this explicitly and preserve danger awareness. Movement, slot selection, aiming, and combat remain active while the panel is open; typing in a form field suppresses only the keys consumed by that field.

Visible state is reactive: progression, health/resource, zone, safety locks,
loadout replies, inventory/crafting replies, and selected-entity snapshots update
the open panel without requiring it to be closed and reopened. Rendering is
coalesced to one animation frame and leaves an actively focused form control in
place until interaction finishes.

Tabs use delegated touch pointer-up activation in addition to ordinary click,
remain at least 44 px tall, and allow horizontal panning of the tab strip. This
avoids depending on iOS's delayed compatibility click while preserving mouse
and keyboard activation.

## Information architecture

| Section | Contents | Availability |
|---|---|---|
| Character | Name, class, level, XP, unlock summary | Always |
| Loadout | Weapon, gadgets/spells, keystones, effects, lock reason | View always; edit in safety |
| Inventory & materials | Items, raw materials, type/grade, death risk | Always |
| Crafting | Blueprint slots, parts, cost, result, validation | Safe zones |
| World | Outposts, biome/danger, spawn/travel | View always; actions follow design rules |
| Squad | Members, invites, leadership, loot rule | Conditional; social operations Open |
| Activity | Boss rank/contribution and contextual progress | Conditional |
| Reference | Combat resources, death, loot, danger, controls | Always |
| Settings | Input, audio, graphics, UI, accessibility, account-safe preferences | Always |
| Admin | Tuning-driven entity placement, selection/editing, graceful delete mode, and material and level grants | Configured administrators only |

Marketplace, guild/territory, monetization, and onboarding are deferred. Add them only when their game rules exist, following the [`README`](README.md#extension-contract) extension contract.

## Administrator developer mode

An administrator sees an Admin tab only after the server has confirmed their
account role. It contains a searchable spawn catalog and generic selected-entity
editor built from each archetype's admin metadata. Off, spawn, select, and delete
pointer modes are explicit toggles. The menu stays open during pointer actions;
a compact HUD states the current mode and offers an immediate exit. Delete mode
fades every entity and keeps connected players compatible with death/respawn.
Positions use adjacent X/Y inputs and a “pick from world” action that minimizes
the panel for the next click. Headings use a rotation slider with a directional
indicator. Plain numeric admin inputs omit browser bounds while the server still
enforces tuning bounds.

The client may never treat the tab or HUD as authority: the server verifies the
administrator session, character ownership, catalog row, field values, and
world coordinate for every action. Non-administrators have neither the tab nor
the protected API capability. See [`../../administration.md`](../../administration.md).

## Exit and session actions

**Exit game** is clear and separated from settings and account destruction. It:

1. explains known vulnerability, material, or position consequences;
2. confirms when leaving creates risk;
3. resists accidental activation;
4. disconnects cleanly and returns Home without signing out.

Sign out belongs on Home. Browser closure and network loss use the same server-authoritative disconnect rules. Logout delay, safety, and combat logging are **Open**.
