// The client's view of the equipped set. It mirrors server/internal/loadout so
// the menu can lay out slots, offer only equippable content, and explain a
// rejection before the round trip — but the server remains the authority: a
// commit is only real once its Loadout reply confirms it.
import { elements, gadgets, loadoutTable, spells, weapons } from "../tuning";
import type { CharacterClass, LoadoutSet } from "../types";

export type SlotKind = "weapon" | "gadget" | "spell";

export interface Slot { index: number; kind: SlotKind; id: string; name: string; element: string }

/** Selectable action-bar slots, bound to 1–6. One width for both classes. */
export const barSlots = loadoutTable.spell_slots;

/** How many other spells of the same element a tier-N spell needs beside it. */
export function requiredSameElement(tier: number): number {
  return tier <= 1 ? 0 : (tier - 1) * loadoutTable.affinity.same_element_per_tier;
}

/** The equipped set laid out in binding order. */
export function bar(characterClass: CharacterClass, set: LoadoutSet): Slot[] {
  if (characterClass === "gunslinger") {
    const weapon = weapons[set.weapon];
    const slots: Slot[] = [{ index: 0, kind: "weapon", id: weapon ? set.weapon : "", name: weapon?.name ?? "", element: "" }];
    for (let index = 0; index < loadoutTable.gadget_slots; index++) {
      const gadget = gadgets[set.gadgets[index] ?? ""];
      slots.push({ index: slots.length, kind: "gadget", id: gadget ? set.gadgets[index]! : "", name: gadget?.name ?? "", element: "" });
    }
    return slots;
  }
  const staff = weapons[set.weapon];
  const slots: Slot[] = [];
  for (let index = 0; index < loadoutTable.spell_slots; index++) {
    // A Mage's empty first slot falls back to the staff's own spell, so a set
    // emptied by a content withdrawal can still fight.
    const id = set.spells[index] || (index === 0 ? staff?.spell ?? "" : "");
    const spell = spells[id];
    slots.push({ index, kind: "spell", id: spell ? id : "", name: spell?.name ?? "", element: spell?.element ?? "" });
  }
  return slots;
}

/** The set a character fights with before it has chosen one. */
export function defaultLoadout(characterClass: CharacterClass): LoadoutSet {
  const set: LoadoutSet = {
    weapon: Object.keys(weapons).find((id) => weapons[id]!.starter && weapons[id]!.class === characterClass) ?? "",
    gadgets: new Array<string>(loadoutTable.gadget_slots).fill(""),
    spells: new Array<string>(loadoutTable.spell_slots).fill(""),
  };
  const starters = characterClass === "gunslinger"
    ? Object.keys(gadgets).filter((id) => gadgets[id]!.starter && gadgets[id]!.class === characterClass).sort()
    : Object.keys(spells).filter((id) => spells[id]!.starter).sort();
  const target = characterClass === "gunslinger" ? set.gadgets : set.spells;
  starters.slice(0, target.length).forEach((id, index) => { target[index] = id; });
  return set;
}

/** Content of a slot kind the character may choose from, in stable order. */
export function equippable(characterClass: CharacterClass, kind: SlotKind): string[] {
  if (kind === "weapon") return Object.keys(weapons).filter((id) => weapons[id]!.class === characterClass).sort();
  if (kind === "gadget") return Object.keys(gadgets).filter((id) => gadgets[id]!.class === characterClass).sort();
  return characterClass === "mage" ? Object.keys(spells).sort() : [];
}

/** Display name of equippable content, whatever kind it is. */
export function contentName(kind: SlotKind, id: string): string {
  if (kind === "weapon") return weapons[id]?.name ?? id;
  if (kind === "gadget") return gadgets[id]?.name ?? id;
  return spells[id]?.name ?? id;
}

/** How many more same-element spells a slot's tier needs. Zero when satisfied. */
export function affinityShortfall(equipped: string[], index: number): number {
  const spell = spells[equipped[index] ?? ""];
  if (!spell) return 0;
  const company = equipped.filter((id, other) => other !== index && id && spells[id]?.element === spell.element).length;
  return Math.max(0, requiredSameElement(spell.tier) - company);
}

/**
 * Why a set may not be committed, or undefined when it is legal. This mirrors
 * the server's rules so the menu can refuse before spending a round trip; the
 * server still validates and its answer wins.
 */
export function loadoutProblem(characterClass: CharacterClass, set: LoadoutSet): string | undefined {
  const weapon = weapons[set.weapon];
  if (!weapon) return "Choose a weapon.";
  if (weapon.class !== characterClass) return `${weapon.name} is a ${weapon.class} weapon.`;
  const equipped = characterClass === "gunslinger" ? set.gadgets : set.spells;
  const kind: SlotKind = characterClass === "gunslinger" ? "gadget" : "spell";
  const seen = new Set<string>();
  for (const id of equipped) {
    if (!id) continue;
    if (seen.has(id)) return `${contentName(kind, id)} is already equipped in another slot.`;
    seen.add(id);
  }
  for (let index = 0; index < set.spells.length; index++) {
    const shortfall = affinityShortfall(set.spells, index);
    if (!shortfall) continue;
    const spell = spells[set.spells[index]!]!;
    const element = elements[spell.element]?.name ?? spell.element;
    return `${spell.name} is tier ${spell.tier}, so it needs ${shortfall} more ${element} spell${shortfall === 1 ? "" : "s"} beside it.`;
  }
  return undefined;
}
