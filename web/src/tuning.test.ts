import { describe, expect, it } from "vitest";
import { combat, damageBandFor, dangerBandAt, projectileByKind, pvpRadius, resourceMax, safeRadius, shotFor, simulation, spells, starterWeapon, weapons, world } from "./tuning";

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

  it("gives both classes a starter weapon that resolves to a dodgeable shot", () => {
    for (const characterClass of ["gunslinger", "mage"] as const) {
      const weapon = starterWeapon(characterClass);
      expect(weapon.class).toBe(characterClass);
      const shot = shotFor(weapon);
      expect(shot.intervalMS).toBeGreaterThan(0);
      expect(shot.damage).toBe(damageBandFor(weapon).damage_per_hit);
      expect(shot.projectile.speed).toBeGreaterThan(0);
    }
  });

  it("puts both starter items on the same shared damage band row", () => {
    const rifle = shotFor(starterWeapon("gunslinger"));
    const staff = shotFor(starterWeapon("mage"));
    expect(rifle.damage).toBe(staff.damage);
    expect(rifle.damage).toBe(combat.damage_bands.standard!.damage_per_hit);
  });

  it("meters the resource the equipped weapon actually spends", () => {
    expect(resourceMax("gunslinger")).toEqual({ label: "Ammo", max: weapons["starter-rifle"]!.magazine_size });
    expect(resourceMax("mage")).toEqual({ label: "Mana", max: combat.player.max_mana });
  });

  it("resolves a projectile silhouette from the kind a snapshot carries", () => {
    expect(projectileByKind("bullet")).toMatchObject({ silhouette: "round" });
    expect(projectileByKind(spells["fire-bolt"]!.projectile.kind)).toMatchObject({ silhouette: "bolt" });
    expect(projectileByKind("unknown")).toBeUndefined();
  });

  it("keeps the snapshot rate an even divisor of the tick rate", () => {
    expect(simulation.tick_rate % simulation.send_rate).toBe(0);
  });
});
