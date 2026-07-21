# In-game menu

The menu opens from a persistent control and conventional keyboard/controller binding. It overlays desktop and may fill mobile. The shared world **does not pause**.

State this explicitly and preserve danger awareness where possible. Menu interaction suspends movement/combat input unless a specific non-blocking panel says otherwise.

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

Marketplace, guild/territory, monetization, and onboarding are deferred. Add them only when their game rules exist, following the [`README`](README.md#extension-contract) extension contract.

## Exit and session actions

**Exit game** is clear and separated from settings and account destruction. It:

1. explains known vulnerability, material, or position consequences;
2. confirms when leaving creates risk;
3. resists accidental activation;
4. disconnects cleanly and returns Home without signing out.

Sign out belongs on Home. Browser closure and network loss use the same server-authoritative disconnect rules. Logout delay, safety, and combat logging are **Open**.
