// The client's view of recipe-blueprint crafting. It mirrors
// server/internal/crafting so the menu can resolve parts, lay out required
// blanks, price a build, and explain a shortfall before the round trip —
// but the server remains the authority: a craft is only real once its Craft
// reply confirms it, and only the server may spend a material.
import { ammunition, components, materials, weapons, type Component, type CraftRecipe, type Weapon } from "../tuning";
import type { CharacterClass, CraftedItem, LoadoutSet } from "../types";
import { locked, type Ledger, type LockedContent } from "./loadout";

/** The weapon categories a character may build: the rows its ledger owns. */
export function craftable(characterClass: CharacterClass, ledger: Ledger): string[] {
  return Object.keys(weapons).filter((id) => weapons[id]!.class === characterClass && ledger.has(id)).sort();
}

/**
 * The weapon categories the character cannot build yet, with the level that
 * grants each. The crafting surface lists them disabled beside what is
 * buildable, so the heavy categories a Gunslinger is working toward are visible
 * before they are reachable rather than appearing out of nowhere.
 */
export function lockedCraftable(characterClass: CharacterClass, ledger: Ledger): LockedContent[] {
  return locked(characterClass, ledger, "weapon");
}

/** The special-ammunition recipes a class may build. These are not ledger-gated. */
export function buildableAmmunition(characterClass: CharacterClass): string[] {
  return Object.keys(ammunition).filter((id) => ammunition[id]!.class === characterClass).sort();
}

/** The component slots a weapon exposes, in blueprint order. */
export function slotsOf(weaponID: string): string[] {
  const recipe = components.recipes[weaponID];
  return recipe ? components.blueprints[recipe.blueprint]?.slots ?? [] : [];
}

export function recipeOf(weaponID: string): CraftRecipe | undefined { return components.recipes[weaponID]; }

/** The component recipes accepted by one blank, including staff tier rules. */
export function fitting(weaponID: string, slot: string, chosen: Record<string, string> = {}): string[] {
  const accepted = [...(components.recipes[weaponID]?.slots[slot] ?? [])];
  if (components.recipes[weaponID]?.blueprint !== "staff") return accepted.sort();
  const crystal = components.components[chosen.crystal ?? ""];
  const stave = components.components[chosen.stave ?? ""];
  return accepted.filter((id) => {
    const part = components.components[id];
    if (!part) return false;
    if (slot === "stave" && crystal) return part.tier >= crystal.tier;
    if (slot === "crystal" && stave) return stave.tier >= part.tier;
    return true;
  }).sort();
}

export function componentOf(id: string): Component | undefined { return components.components[id]; }

/** The one recipe a complete arrangement resolves to; incomplete/unknown is undefined. */
export function resultOf(chosen: Record<string, string>): string | undefined {
  const matches = Object.keys(components.recipes).filter((weaponID) => {
    const recipe = components.recipes[weaponID]!;
    const slots = components.blueprints[recipe.blueprint]?.slots ?? [];
    if (Object.keys(chosen).length !== slots.length || !slots.every((slot) => recipe.slots[slot]?.includes(chosen[slot] ?? ""))) return false;
    if (recipe.blueprint === "staff") {
      const crystal = components.components[chosen.crystal ?? ""], stave = components.components[chosen.stave ?? ""];
      if (!crystal || !stave || stave.tier < crystal.tier) return false;
    }
    return true;
  });
  return matches.length === 1 ? matches[0] : undefined;
}

/** Display name of a material, falling back to its ID for unknown content. */
export function materialName(id: string): string { return materials.materials[id]?.name ?? id; }

/**
 * What one build consumes, as material ID → count: the weapon row's own cost
 * plus every component filling a slot. Most categories are free; the heavy ones
 * carry a cost of their own, which is how rare materials gate them.
 */
export function cost(weaponID: string, chosen: Record<string, string>): Record<string, number> {
  const total: Record<string, number> = {};
  for (const [material, count] of Object.entries(weapons[weaponID]?.cost ?? {})) {
    total[material] = (total[material] ?? 0) + count;
  }
  for (const id of Object.values(chosen)) {
    for (const [material, count] of Object.entries(components.components[id]?.cost ?? {})) {
      total[material] = (total[material] ?? 0) + count;
    }
  }
  return total;
}

/** What is still missing to pay a cost, per material, and empty when affordable. */
export function shortfall(required: Record<string, number>, carried: Record<string, number>): Record<string, number> {
  const short: Record<string, number> = {};
  for (const [material, count] of Object.entries(required)) {
    const missing = count - (carried[material] ?? 0);
    if (missing > 0) short[material] = missing;
  }
  return short;
}

/**
 * Plain-language behavior of a build, one line per filled slot in blueprint
 * order. This is what the crafting surface shows instead of raw multipliers.
 */
export function describe(weaponID: string, chosen: Record<string, string>): string[] {
  const lines: string[] = [];
  for (const slot of slotsOf(weaponID)) {
    const component = components.components[chosen[slot] ?? ""];
    if (component) lines.push(`${component.name}: ${component.effect}`);
  }
  return lines;
}

/** What an owned item is called and how it is configured, for a one-line label. */
export function itemLabel(item: CraftedItem): string {
  const name = weapons[item.weapon]?.name ?? item.weapon;
  const parts = Object.keys(item.components).sort()
    .map((slot) => components.components[item.components[slot]!]?.name)
    .filter((part): part is string => Boolean(part));
  return parts.length ? `${name} — ${parts.join(", ")}` : `${name} (stock)`;
}

/** The crafted items whose weapon row this character may still equip. */
export function equippableItems(characterClass: CharacterClass, ledger: Ledger, items: readonly CraftedItem[]): CraftedItem[] {
  return items
    .filter((item) => weapons[item.weapon]?.class === characterClass && ledger.has(item.weapon))
    .sort((left, right) => left.id.localeCompare(right.id));
}

/**
 * Merged multipliers of a build: two components touching one attribute
 * multiply, exactly as the server merges them.
 */
export function modifiersOf(chosen: Record<string, string>): Record<string, number> {
  const merged: Record<string, number> = {};
  for (const id of Object.values(chosen)) {
    for (const [attribute, modifier] of Object.entries(components.components[id]?.modifiers ?? {})) {
      merged[attribute] = (merged[attribute] ?? 1) * modifier;
    }
  }
  return merged;
}

/**
 * The weapon row an equipped reference resolves to, with its components applied
 * to the attributes the HUD reads. The server derives the authoritative values
 * the same way from the same tables; this only keeps the meter honest between
 * snapshots.
 */
export function resolvedWeapon(id: string, items: readonly CraftedItem[]): Weapon | undefined {
  const item = items.find((owned) => owned.id === id);
  const weapon = weapons[item ? item.weapon : id];
  if (!weapon || !item) return weapon;
  const modifiers = modifiersOf(item.components);
  if (!weapon.magazine_size) return weapon;
  return {
    ...weapon,
    magazine_size: Math.max(1, Math.round(weapon.magazine_size * (modifiers.magazine_size ?? 1))),
    reload_ms: Math.max(1, Math.round((weapon.reload_ms ?? 0) * (modifiers.reload_ms ?? 1))),
  };
}

/** Whether an equipped weapon reference names a crafted instance. */
export function equippedItem(set: LoadoutSet, items: readonly CraftedItem[]): CraftedItem | undefined {
  return items.find((item) => item.id === set.weapon);
}
