import { describe, expect, it } from "vitest";
import { bar, barSlots, defaultLoadout, equippable, loadoutProblem, requiredSameElement } from "./loadout";
import { loadoutTable } from "../tuning";

describe("loadout slots", () => {
  it("lays both classes out over the same six bindings", () => {
    const gunslinger = bar("gunslinger", defaultLoadout("gunslinger"));
    const mage = bar("mage", defaultLoadout("mage"));
    expect(gunslinger).toHaveLength(barSlots);
    expect(mage).toHaveLength(barSlots);
    expect(gunslinger[0]!.kind).toBe("weapon");
    expect(gunslinger.slice(1).every((slot) => slot.kind === "gadget")).toBe(true);
    expect(mage.every((slot) => slot.kind === "spell")).toBe(true);
  });

  it("arms both starter kits from slot one", () => {
    expect(bar("gunslinger", defaultLoadout("gunslinger"))[0]!.name).toBeTruthy();
    expect(bar("mage", defaultLoadout("mage"))[0]!.name).toBeTruthy();
  });

  it("mirrors the server's affinity rule from the table, not a literal", () => {
    expect(requiredSameElement(1)).toBe(0);
    expect(requiredSameElement(4)).toBe(3 * loadoutTable.affinity.same_element_per_tier);
  });

  it("refuses a set the server would refuse", () => {
    expect(loadoutProblem("mage", defaultLoadout("mage"))).toBeUndefined();
    expect(loadoutProblem("mage", { weapon: "", gadgets: [], spells: [] })).toBeTruthy();
    expect(loadoutProblem("gunslinger", { weapon: "starter-staff", gadgets: [], spells: [] })).toBeTruthy();
    // The same spell in two slots cannot pad its own affinity requirement.
    expect(loadoutProblem("mage", { weapon: "starter-staff", gadgets: [], spells: ["fire-bolt", "fire-bolt"] })).toBeTruthy();
  });

  it("offers only content of the character's own slot kind", () => {
    expect(equippable("gunslinger", "spell")).toHaveLength(0);
    expect(equippable("mage", "weapon").every((id) => id !== "starter-rifle")).toBe(true);
  });
});
