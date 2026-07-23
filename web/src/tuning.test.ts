import { describe, expect, it } from "vitest";
import { abilities, abilityFor, deployableByKind, combat, damageBandFor, dangerBandAt, damageOf, entityDefinitions, materials, projectileByKind, pvpRadius, resourceMax, safeRadius, simulation, spells, starterWeapon, weapons, world } from "./tuning";

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

  it("exposes sustained, burst, and heavy-burst anchors with their cadence", () => {
    expect(Object.keys(combat.damage_bands).sort()).toEqual(["burst", "heavy-burst", "sustained"]);
    for (const band of Object.values(combat.damage_bands)) {
      expect(band.damage_per_hit).toBeGreaterThan(0);
      expect(band.interval_ms).toBeGreaterThan(0);
      expect(band.target_ttk_seconds).toBeGreaterThanOrEqual(2);
    }
    expect(damageBandFor(weapons["field-pistol"]!).damage_per_hit).toBe(combat.damage_bands.sustained!.damage_per_hit);
    expect(damageBandFor(weapons["long-sniper"]!).damage_per_hit).toBe(combat.damage_bands["heavy-burst"]!.damage_per_hit);
  });

  it("charges the mage mana and the gunslinger ammunition through one ability shape", () => {
    expect(abilityFor(starterWeapon("gunslinger")).cost).toEqual({ kind: "ammo", amount: 1 });
    expect(abilityFor(starterWeapon("mage")).cost.kind).toBe("mana");
  });

  it("meters the resource the equipped weapon actually spends", () => {
    const gun = starterWeapon("gunslinger");
    expect(resourceMax(gun)).toEqual({ label: "Ammo", max: gun.magazine_size, capped: true });
    expect(resourceMax(starterWeapon("mage"))).toEqual({ label: "Mana", max: combat.player.max_mana, capped: true });
    // A weapon that spends crafted ammunition meters what it carries instead of
    // a magazine, so the HUD never shows a reload that will never come.
    expect(resourceMax(weapons["field-launcher"])).toMatchObject({ label: "Rocket", capped: false });
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
    expect(world.fixtures).toContainEqual({ id: "wall-00", entity: "wall", position: [2400, 0] });
  });

  it("exposes spawn and edit metadata on entity archetypes", () => {
    expect(Object.values(entityDefinitions).every((definition) => definition.admin.spawnable)).toBe(true);
    for (const definition of Object.values(entityDefinitions)) for (const field of definition.admin.fields) {
      expect(field.attribute).toContain(".");
      expect(["number", "text", "select", "position", "rotation"]).toContain(field.input);
    }
    expect(entityDefinitions.player!.admin.fields.some((field) => field.attribute === "transform.position" && field.input === "position" && field.scope === "edit")).toBe(true);
    expect(materials.admin_grant.attribute).toBe("inventory.material_count");
  });
});

describe("spells", () => {
  it("resolves a field's identity from the ability that places it", () => {
    // Only a concealing field takes anything off the wire, and only that one may
    // be drawn over the bodies inside it.
    expect(deployableByKind("smoke")?.conceals).toBe(true);
    expect(deployableByKind("cinder")?.conceals).toBeFalsy();
    expect(deployableByKind("cinder")?.damage_band).toBeTruthy();
  });

  it("prices every spell in mana, and every defining spell in cooldown too", () => {
    for (const [id, spell] of Object.entries(spells)) {
      const ability = abilities[spell.ability]!;
      expect(ability.cost.kind, id).toBe("mana");
      expect(ability.cost.amount, id).toBeGreaterThan(0);
      if (spell.tier > 1) expect(ability.cooldown_ms, id).toBeGreaterThan(0);
    }
  });
});
