// The client's view of slotted-blueprint crafting. It mirrors
// server/internal/crafting so the menu can lay out slots, offer only fitting
// components, price a build, and explain a shortfall before the round trip —
// but the server remains the authority: a craft is only real once its Craft
// reply confirms it, and only the server may spend a material.
import { components, materials, weapons, type Component, type Weapon } from "../tuning";
import type { CharacterClass, CraftedItem, LoadoutSet } from "../types";
import type { Ledger } from "./loadout";

/** The weapon categories a character may build: the rows its ledger owns. */
export function craftable(characterClass: CharacterClass, ledger: Ledger): string[] {
  return Object.keys(weapons).filter((id) => weapons[id]!.class === characterClass && ledger.has(id)).sort();
}

/** The component slots a weapon exposes, in blueprint order. */
export function slotsOf(weaponID: string): string[] {
  const weapon = weapons[weaponID];
  return weapon ? components.blueprints[weapon.blueprint]?.slots ?? [] : [];
}

/** The components that fit one slot of a weapon, in stable order. */
export function fitting(weaponID: string, slot: string): string[] {
  const blueprint = weapons[weaponID]?.blueprint;
  if (!blueprint) return [];
  return Object.keys(components.components)
    .filter((id) => components.components[id]!.blueprint === blueprint && components.components[id]!.slot === slot)
    .sort();
}

export function componentOf(id: string): Component | undefined { return components.components[id]; }

/** Display name of a material, falling back to its ID for unknown content. */
export function materialName(id: string): string { return materials.materials[id]?.name ?? id; }

/** What a set of component choices consumes, as material ID → count. */
export function cost(chosen: Record<string, string>): Record<string, number> {
  const total: Record<string, number> = {};
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
 * Plain-language behaviour of a build, one line per filled slot in blueprint
 * order. This is what the crafting surface shows instead of a table of
 * multipliers — a rare part must never read as a higher power tier.
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
