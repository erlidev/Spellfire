// The client's view of the equipped set. It mirrors server/internal/loadout so
// the menu can lay out slots, offer only equippable content, and explain a
// rejection before the round trip — but the server remains the authority: a
// commit is only real once its Loadout reply confirms it.
import { elements, gadgets, loadoutTable, spells, weapons } from "../tuning";
import type { CharacterClass, CraftedItem, LoadoutSet } from "../types";
import { equippableItems, itemLabel } from "./crafting";

export type SlotKind = "weapon" | "gadget" | "spell";

/** One selectable position, mirroring server/internal/loadout.Slot. `abilityId`
 * is what the use button performs, so nothing downstream has to know which
 * table the slot's content came from. */
export interface Slot { index: number; kind: SlotKind; id: string; name: string; element: string; abilityId: string }

/**
 * Resolves an equipped weapon reference to the row it fights with. The slot
 * holds either a stock weapon row or a crafted instance of one; the server
 * resolves the same two cases, so the menu never offers something it would
 * refuse.
 */
export function equippedWeapon(id: string, items: readonly CraftedItem[]): { row: string; item?: CraftedItem } | undefined {
  const item = items.find((owned) => owned.id === id);
  if (item) return weapons[item.weapon] ? { row: item.weapon, item } : undefined;
  // A row the economy withholds has no stock configuration: it can only be
  // carried as an instance that was actually built.
  return weapons[id] && !weapons[id]!.requires_craft ? { row: id } : undefined;
}

/** Selectable action-bar slots, bound to 1–6. One width for both classes. */
export const barSlots = loadoutTable.spell_slots;

/** How many other spells of the same element a tier-N spell needs beside it. */
export function requiredSameElement(tier: number): number {
  return tier <= 1 ? 0 : (tier - 1) * loadoutTable.affinity.same_element_per_tier;
}

/** The equipped set laid out in binding order. */
export function bar(characterClass: CharacterClass, set: LoadoutSet, items: readonly CraftedItem[] = []): Slot[] {
  const equipped = equippedWeapon(set.weapon, items);
  const weapon = equipped ? weapons[equipped.row] : undefined;
  if (characterClass === "gunslinger") {
    const slots: Slot[] = [{ index: 0, kind: "weapon", id: weapon ? set.weapon : "", name: weapon?.name ?? "", element: "", abilityId: weapon?.ability ?? "" }];
    for (let index = 0; index < loadoutTable.gadget_slots; index++) {
      const gadget = gadgets[set.gadgets[index] ?? ""];
      slots.push({ index: slots.length, kind: "gadget", id: gadget ? set.gadgets[index]! : "", name: gadget?.name ?? "", element: "", abilityId: gadget?.ability ?? "" });
    }
    return slots;
  }
  const staff = weapon;
  const slots: Slot[] = [];
  for (let index = 0; index < loadoutTable.spell_slots; index++) {
    // A Mage's empty first slot falls back to the staff's own spell, so a set
    // emptied by a content withdrawal can still fight.
    const id = set.spells[index] || (index === 0 ? staff?.spell ?? "" : "");
    const spell = spells[id];
    slots.push({ index, kind: "spell", id: spell ? id : "", name: spell?.name ?? "", element: spell?.element ?? "", abilityId: spell?.ability ?? "" });
  }
  return slots;
}

/**
 * The set a character fights with before the server's authoritative one
 * arrives: what its ledger owns, packed from slot zero. The server resolves the
 * same shape, so the menu is not showing content the world would refuse.
 */
export function defaultLoadout(characterClass: CharacterClass, ledger: Ledger): LoadoutSet {
  const set: LoadoutSet = {
    // Stock rows only: the default is the plain configuration, and which crafted
    // weapon to carry is a choice the player makes.
    weapon: equippable(characterClass, ledger, "weapon")[0] ?? "",
    gadgets: new Array<string>(loadoutTable.gadget_slots).fill(""),
    spells: new Array<string>(loadoutTable.spell_slots).fill(""),
  };
  const kind: SlotKind = characterClass === "gunslinger" ? "gadget" : "spell";
  const target = characterClass === "gunslinger" ? set.gadgets : set.spells;
  equippable(characterClass, ledger, kind).slice(0, target.length).forEach((id, index) => { target[index] = id; });
  dropIllegal(set.spells);
  return set;
}

/**
 * Unequips whatever keeps a set from validating, highest slot first, exactly as
 * the server's resolve does: packing every owned spell into the bar will often
 * break affinity, and the deterministic casualty is the last thing equipped
 * rather than the character's signature.
 */
function dropIllegal(spells: string[]): void {
  for (let pass = 0; pass < spells.length; pass++) {
    let worst = -1;
    for (let index = spells.length - 1; index >= 0 && worst < 0; index--) {
      if (spells[index] && affinityShortfall(spells, index) > 0) worst = index;
    }
    if (worst < 0) return;
    spells[worst] = "";
  }
}

/** The flat permanent unlock ledger, as the client holds it. */
export type Ledger = ReadonlySet<string>;

export function ledgerOf(unlocks: readonly string[]): Ledger { return new Set(unlocks); }

/**
 * Content of a slot kind the character may choose from, in stable order: the
 * live rows of its class that its ledger owns. The server enforces the same
 * intersection — hiding an option is never what stops it being equipped.
 */
export function equippable(characterClass: CharacterClass, ledger: Ledger, kind: SlotKind, items: readonly CraftedItem[] = []): string[] {
  const owns = (id: string) => ledger.has(id);
  if (kind === "weapon") {
    // Stock rows first, then the crafted instances of them, so the plain
    // configuration stays the deterministic default.
    const stock = Object.keys(weapons).filter((id) => weapons[id]!.class === characterClass && !weapons[id]!.requires_craft && owns(id)).sort();
    return [...stock, ...equippableItems(characterClass, ledger, items).map((item) => item.id)];
  }
  if (kind === "gadget") return Object.keys(gadgets).filter((id) => gadgets[id]!.class === characterClass && owns(id)).sort();
  return characterClass === "mage" ? Object.keys(spells).filter(owns).sort() : [];
}

/**
 * Content of a slot kind the character has not unlocked yet, with the level
 * that grants it. The menu shows these beside what is equippable, disabled: a
 * player who cannot see that a gadget exists has no reason to believe the slot
 * will ever fill, and progression is only motivating if it is legible.
 */
export function locked(characterClass: CharacterClass, ledger: Ledger, kind: SlotKind): LockedContent[] {
  const rows: Record<string, { name: string; unlock_level: number; class?: CharacterClass }> =
    kind === "weapon" ? weapons : kind === "gadget" ? gadgets : (characterClass === "mage" ? spells : {});
  return Object.keys(rows)
    .filter((id) => !ledger.has(id) && (rows[id]!.class ?? characterClass) === characterClass)
    .map((id) => ({ id, name: rows[id]!.name, level: rows[id]!.unlock_level }))
    .sort((left, right) => left.level - right.level || left.id.localeCompare(right.id));
}

export interface LockedContent { id: string; name: string; level: number }

/** Display name of equippable content, whatever kind it is. */
export function contentName(kind: SlotKind, id: string, items: readonly CraftedItem[] = []): string {
  if (kind === "weapon") {
    const item = items.find((owned) => owned.id === id);
    if (item) return itemLabel(item);
    return weapons[id]?.name ?? id;
  }
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
export function loadoutProblem(characterClass: CharacterClass, ledger: Ledger, set: LoadoutSet, items: readonly CraftedItem[] = []): string | undefined {
  const held = equippedWeapon(set.weapon, items);
  const weapon = held ? weapons[held.row] : undefined;
  if (!weapon || !held) {
    const withheld = weapons[set.weapon];
    if (withheld?.requires_craft) return `${withheld.name} has to be built before it can be carried.`;
    return "Choose a weapon.";
  }
  if (weapon.class !== characterClass) return `${weapon.name} is a ${weapon.class} weapon.`;
  if (!ledger.has(held.row)) return `You have not unlocked ${weapon.name}.`;
  const equipped = characterClass === "gunslinger" ? set.gadgets : set.spells;
  const kind: SlotKind = characterClass === "gunslinger" ? "gadget" : "spell";
  const seen = new Set<string>();
  for (const id of equipped) {
    if (!id) continue;
    if (!ledger.has(id)) return `You have not unlocked ${contentName(kind, id)}.`;
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
