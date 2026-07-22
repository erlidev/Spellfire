import { describe, expect, it } from "vitest";
import { cost, describe as describeBuild, fitting, recipeOf, resolvedWeapon, resultOf, shortfall, slotsOf } from "./crafting";
import { bar, equippable, ledgerOf } from "./loadout";
import { components, weapons } from "../tuning";
import type { CraftedItem } from "../types";

const everything = ledgerOf(Object.keys(weapons));
const gun = Object.keys(weapons).find((id) => weapons[id]!.class === "gunslinger" && weapons[id]!.blueprint === "gun")!;

/** The first live component filling a slot, so a rename does not break a test. */
function componentIn(slot: string): string {
  const [id] = fitting(gun, slot);
  if (!id) throw new Error(`no component fills gun/${slot}`);
  return id;
}

describe("crafting recipes", () => {
  it("exposes the blueprint's slots for the chosen category", () => {
    expect(slotsOf(gun)).toEqual(components.blueprints[recipeOf(gun)!.blueprint]!.slots);
    expect(slotsOf("not-a-weapon")).toEqual([]);
  });

  it("offers only the components that fit a slot", () => {
    for (const slot of slotsOf(gun)) {
      for (const id of fitting(gun, slot)) {
        expect(components.components[id]!.slot).toBe(slot);
        expect(recipeOf(gun)!.slots[slot]).toContain(id);
      }
    }
  });

  it("adds the cost of every filled slot rather than overwriting it", () => {
    const receiver = componentIn("receiver"), barrel = componentIn("barrel");
    const total = cost(gun, { receiver, barrel });
    for (const [material, count] of Object.entries(components.components[receiver]!.cost)) {
      expect(total[material]).toBe(count + (components.components[barrel]!.cost[material] ?? 0));
    }
    expect(cost(gun, {})).toEqual({});
    // A heavy category's own material cost is charged even with every slot stock.
    expect(cost("long-sniper", {})).not.toEqual({});
  });

  it("names what is missing instead of refusing as a whole", () => {
    expect(shortfall({ "salvaged-plate": 5 }, { "salvaged-plate": 3 })).toEqual({ "salvaged-plate": 2 });
    expect(shortfall({ "salvaged-plate": 5 }, { "salvaged-plate": 5 })).toEqual({});
  });

  it("states behaviour in plain language, one line per filled slot", () => {
    const receiver = componentIn("receiver");
    const lines = describeBuild(gun, { receiver });
    expect(lines).toHaveLength(1);
    expect(lines[0]).toContain(components.components[receiver]!.name);
    expect(describeBuild(gun, {})).toEqual([]);
  });

  it("derives the resulting weapon from a complete arrangement", () => {
    const chosen: Record<string, string> = {};
    for (const slot of slotsOf(gun)) chosen[slot] = fitting(gun, slot)[0]!;
    expect(resultOf(chosen)).toBe(gun);
    delete chosen.barrel;
    expect(resultOf(chosen)).toBeUndefined();
  });
});

describe("crafted weapons on the bar", () => {
  const item: CraftedItem = { id: "itm-1", weapon: gun, components: { feed: componentIn("feed") } };

  it("resolves an equipped instance to its category and applies its components", () => {
    const resolved = resolvedWeapon(item.id, [item])!;
    const stock = weapons[gun]!;
    const modifier = components.components[item.components.feed!]!.modifiers.magazine_size;
    expect(resolved.name).toBe(stock.name);
    if (modifier && stock.magazine_size) expect(resolved.magazine_size).not.toBe(stock.magazine_size);
    expect(resolvedWeapon(gun, [item])).toEqual(stock);
  });

  it("puts the instance in the weapon slot and stock rows first in the menu", () => {
    const slots = bar("gunslinger", { weapon: item.id, gadgets: [], spells: [], keystones: [] }, [item]);
    expect(slots[0]!.kind).toBe("weapon");
    expect(slots[0]!.name).toBe(weapons[gun]!.name);
    const options = equippable("gunslinger", everything, "weapon", [item]);
    expect(options[options.length - 1]).toBe(item.id);
    expect(options[0]).not.toBe(item.id);
  });
});
