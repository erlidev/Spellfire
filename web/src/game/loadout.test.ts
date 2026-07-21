import { describe, expect, it } from "vitest";
import { bar, barSlots, defaultLoadout, equippable, ledgerOf, loadoutProblem, requiredSameElement } from "./loadout";
import { gadgets, loadoutTable, spells, weapons } from "../tuning";

// A character that owns every live row. The ledger is the axis under test only
// where a test says so; everywhere else it must not be the reason a case passes.
const everything = ledgerOf([...Object.keys(weapons), ...Object.keys(spells), ...Object.keys(gadgets)]);

describe("loadout slots", () => {
  it("lays both classes out over the same six bindings", () => {
    const gunslinger = bar("gunslinger", defaultLoadout("gunslinger", everything));
    const mage = bar("mage", defaultLoadout("mage", everything));
    expect(gunslinger).toHaveLength(barSlots);
    expect(mage).toHaveLength(barSlots);
    expect(gunslinger[0]!.kind).toBe("weapon");
    expect(gunslinger.slice(1).every((slot) => slot.kind === "gadget")).toBe(true);
    expect(mage.every((slot) => slot.kind === "spell")).toBe(true);
  });

  it("arms both starter kits from slot one", () => {
    expect(bar("gunslinger", defaultLoadout("gunslinger", everything))[0]!.name).toBeTruthy();
    expect(bar("mage", defaultLoadout("mage", everything))[0]!.name).toBeTruthy();
  });

  it("mirrors the server's affinity rule from the table, not a literal", () => {
    expect(requiredSameElement(1)).toBe(0);
    expect(requiredSameElement(4)).toBe(3 * loadoutTable.affinity.same_element_per_tier);
  });

  it("refuses a set the server would refuse", () => {
    expect(loadoutProblem("mage", everything, defaultLoadout("mage", everything))).toBeUndefined();
    expect(loadoutProblem("mage", everything, { weapon: "", gadgets: [], spells: [] })).toBeTruthy();
    expect(loadoutProblem("gunslinger", everything, { weapon: "starter-staff", gadgets: [], spells: [] })).toBeTruthy();
    // The same spell in two slots cannot pad its own affinity requirement.
    expect(loadoutProblem("mage", everything, { weapon: "starter-staff", gadgets: [], spells: ["fire-bolt", "fire-bolt"] })).toBeTruthy();
  });

  it("offers only content of the character's own slot kind", () => {
    expect(equippable("gunslinger", everything, "spell")).toHaveLength(0);
    expect(equippable("mage", everything, "weapon").every((id) => id !== "starter-rifle")).toBe(true);
  });

  it("offers and accepts only what the ledger owns", () => {
    const owned = ledgerOf(["starter-staff"]);
    expect(equippable("mage", owned, "spell")).toHaveLength(0);
    expect(equippable("mage", owned, "weapon")).toEqual(["starter-staff"]);
    // The server refuses unowned content; the menu must say so before the trip.
    expect(loadoutProblem("mage", owned, { weapon: "starter-staff", gadgets: [], spells: ["fire-bolt"] })).toBeTruthy();
    expect(loadoutProblem("mage", ledgerOf([]), { weapon: "starter-staff", gadgets: [], spells: [] })).toBeTruthy();
  });
});
