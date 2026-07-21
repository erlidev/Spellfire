import { describe, expect, it } from "vitest";
import { abilities, abilityFor, adminTools, combat, damageBandFor, dangerBandAt, damageOf, entityDefinitions, projectileByKind, pvpRadius, resourceMax, safeRadius, simulation, spells, starterWeapon, weapons, world } from "./tuning";

describe("shared tuning tables", () => {
  it("derives the safety radii from the danger band rows rather than literals", () => {
    const hub = world.danger_bands[0]!;
    const restricted = world.danger_bands.filter((band) => band.pvp === "off" || band.pvp === "restricted").at(-1)!;
    expect(safeRadius).toBe(hub.outer_radius);
    expect(pvpRadius).toBe(restricted.outer_radius);
    expect(world.danger_bands.at(-1)!.outer_radius).toBe(world.radius);
  });

  it("resolves every radius to a band, clamping past the rim", () => {
    expect(dangerBandAt(0).id).toBe("hub");
    expect(dangerBandAt(safeRadius).id).toBe("hub");
    expect(dangerBandAt(safeRadius + 1).id).toBe("fringe");
    expect(dangerBandAt(world.radius * 2).id).toBe(world.danger_bands.at(-1)!.id);
  });

  it("gives both classes a starter weapon that resolves to a dodgeable ability", () => {
    for (const characterClass of ["gunslinger", "mage"] as const) {
      const weapon = starterWeapon(characterClass);
      expect(weapon.class).toBe(characterClass);
      const ability = abilityFor(weapon);
      expect(ability.interval_ms).toBeGreaterThan(0);
      expect(ability.dodge_vector).toBe("projectile_travel");
      expect(damageOf(ability.damage_band!)).toBe(damageBandFor(weapon).damage_per_hit);
      expect(ability.projectile!.speed).toBeGreaterThan(0);
    }
  });

  it("puts both starter items on the same shared damage band row", () => {
    const rifle = damageBandFor(starterWeapon("gunslinger"));
    const staff = damageBandFor(starterWeapon("mage"));
    expect(rifle.damage_per_hit).toBe(staff.damage_per_hit);
    expect(rifle.damage_per_hit).toBe(combat.damage_bands.standard!.damage_per_hit);
  });

  it("charges the mage mana and the gunslinger ammunition through one ability shape", () => {
    expect(abilityFor(starterWeapon("gunslinger")).cost).toEqual({ kind: "ammo", amount: 1 });
    expect(abilityFor(starterWeapon("mage")).cost.kind).toBe("mana");
  });

  it("meters the resource the equipped weapon actually spends", () => {
    expect(resourceMax(starterWeapon("gunslinger"))).toEqual({ label: "Ammo", max: weapons["starter-rifle"]!.magazine_size });
    expect(resourceMax(starterWeapon("mage"))).toEqual({ label: "Mana", max: combat.player.max_mana });
  });

  it("resolves a projectile silhouette from the kind a snapshot carries", () => {
    expect(projectileByKind("bullet")).toMatchObject({ silhouette: "round" });
    expect(projectileByKind(abilities[spells["fire-bolt"]!.ability]!.projectile!.kind)).toMatchObject({ silhouette: "bolt" });
    expect(projectileByKind("unknown")).toBeUndefined();
  });

  it("keeps the snapshot rate an even divisor of the tick rate", () => {
    expect(simulation.tick_rate % simulation.send_rate).toBe(0);
  });

  it("shares entity mass, health, and collision defaults with the server", () => {
    expect(entityDefinitions.tree).toMatchObject({ mass: -1, max_health: 500, collision_objects: [{ type: "circle" }] });
    expect(entityDefinitions.wall).toMatchObject({ mass: -1, max_health: -1, collision_objects: [{ type: "box", width: 96, height: 96 }] });
    expect(world.fixtures).toContainEqual({ id: "wall-00", entity: "wall", position: [650, 0] });
  });

  it("exposes only live developer-mode entity families through data", () => {
    const kinds = new Set(Object.values(adminTools.spawnables).map((spawnable) => spawnable.kind));
    expect(kinds).toEqual(new Set(["player", "projectile", "telegraph"]));
    for (const spawnable of Object.values(adminTools.spawnables)) for (const field of spawnable.fields) {
      expect(["number", "text"]).toContain(field.kind);
      if (field.kind === "number") expect(field.minimum).toBeLessThanOrEqual(field.default_number!);
    }
    expect(adminTools.attributes.speed_multiplier).toBeDefined();
    expect(adminTools.attributes.view_distance).toBeDefined();
  });
});
