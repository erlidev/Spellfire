import { describe, expect, it } from "vitest";
import { outposts, world } from "../tuning";
import { insideOutpostBubble, nearestOutpost, protectedAt, safeAt, serviceAt } from "./outposts";
import { worldField } from "./worldfield";

// The browser mirror of server/internal/game/outpost.go. If these two disagree,
// the HUD tells a player they are safe where the server will still shoot them.
describe("the outpost overlay", () => {
  const rows = Object.entries(outposts);

  it("ships outposts across the Fringe and Frontier and none in the Deadlands", () => {
    expect(rows.length).toBeGreaterThan(0);
    for (const [id, outpost] of rows) {
      const distance = Math.hypot(outpost.position[0], outpost.position[1]);
      expect(distance, id).toBeLessThanOrEqual(world.radius);
      expect(worldField.dangerAt(outpost.position[0], outpost.position[1]).pvp, id).not.toBe("full");
      expect(outpost.safe_radius, id).toBeGreaterThan(0);
      expect(outpost.discovery_radius, id).toBeGreaterThanOrEqual(outpost.safe_radius);
      expect(outpost.services.length, id).toBeGreaterThan(0);
    }
  });

  it("makes an outpost bubble safe and protected even in a hostile band", () => {
    const [id, outpost] = rows.find(([, o]) => o.band === "frontier")!;
    const [x, y] = outpost.position;
    // The band underfoot is unchanged: an outpost overlays safety, not geography.
    expect(worldField.safeAt(x, y), id).toBe(false);
    expect(insideOutpostBubble(x, y), id).toBe(true);
    expect(safeAt(x, y), id).toBe(true);
    expect(protectedAt(x, y), id).toBe(true);
    // Just outside it, the Frontier is hostile again.
    const outsideX = x + outpost.safe_radius + 50;
    expect(safeAt(outsideX, y), id).toBe(false);
    expect(protectedAt(outsideX, y), id).toBe(false);
  });

  it("offers only the services a row declares, while the hub offers everything", () => {
    expect(serviceAt(0, 0, "loadout")).toBe(true);
    expect(serviceAt(0, 0, "crafting")).toBe(true);
    for (const [id, outpost] of rows) {
      const [x, y] = outpost.position;
      for (const service of ["loadout", "crafting", "respawn"]) {
        expect(serviceAt(x, y, service), `${id}:${service}`).toBe(outpost.services.includes(service));
      }
    }
  });

  it("names the nearest discovered outpost and never an undiscovered one", () => {
    const [id, outpost] = rows[0]!;
    const [x, y] = outpost.position;
    expect(nearestOutpost(x, y, new Set())).toBeUndefined();
    const nearest = nearestOutpost(x, y, new Set([id]));
    expect(nearest?.id).toBe(id);
    expect(nearest?.distance).toBeCloseTo(0, 6);
  });
});
