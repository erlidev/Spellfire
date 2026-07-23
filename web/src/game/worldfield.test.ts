import { describe, expect, it } from "vitest";
import golden from "../../../testdata/worldfield.json";
import { biomes, materials, world } from "../tuning";
import { WorldField, fieldParams, materialsAt, worldField } from "./worldfield";

// The fixture is written by the Go suite. Checking the browser's field against
// it is the only thing holding the two implementations together: the renderer
// tints the ground by biome and the HUD names the region, and a client that
// drew a border in a different place from the one the server simulates would
// be showing a world that does not exist.
describe("the world field", () => {
  it("agrees with the Go field on every golden sample", () => {
    const field = new WorldField({
      radius: golden.params.Radius,
      seed: golden.params.Seed >>> 0,
      regionCell: golden.params.RegionCell,
      regionJitter: golden.params.RegionJitter,
      radialReference: golden.params.RadialReference,
      warpCell: golden.params.WarpCell,
      warpAmplitude: golden.params.WarpAmplitude,
      blendWidth: golden.params.BlendWidth,
      biomeIDs: golden.params.Biomes,
      bands: golden.params.Bands.map((band) => ({
        id: band.ID, name: band.Name, pvp: band.PvP, material_grade: band.MaterialGrade,
        shape: band.Shape, summary: band.Summary, tier: band.Tier, outer_radius: band.OuterRadius,
      })),
      grades: {
        points: golden.params.Grades.Points.map((point) => ({ at: point.At, richness: point.Richness })),
        thresholds: golden.params.Grades.Thresholds.map((threshold) => ({ id: threshold.ID, tier: threshold.Tier, at: threshold.At })),
      },
    });
    for (const sample of golden.samples) {
      const region = field.regionAt(sample.x, sample.y);
      // Exact equality, not a tolerance: the two implementations are written to
      // agree to the bit, and a drift of one ulp is a drift.
      expect({
        region: region.id, danger: region.danger.id,
        biome: region.biome.id, neighbour: region.biome.neighbour, blend: region.biome.blend,
        grade: region.grade.id, tier: region.grade.tier, richness: region.grade.richness,
      }).toEqual({
        region: sample.region, danger: sample.danger,
        biome: sample.biome, neighbour: sample.neighbour, blend: sample.blend,
        grade: sample.grade, tier: sample.tier, richness: sample.richness,
      });
    }
  });

  it("builds the same parameters from the shipped tables as the fixture was made with", () => {
    const params = fieldParams();
    expect(params.radius).toBe(golden.params.Radius);
    expect(params.seed).toBe(golden.params.Seed >>> 0);
    expect(params.regionCell).toBe(golden.params.RegionCell);
    expect(params.radialReference).toBe(golden.params.RadialReference);
    expect(params.biomeIDs).toEqual(golden.params.Biomes);
    expect(params.grades.thresholds.map((threshold) => threshold.id)).toEqual(
      golden.params.Grades.Thresholds.map((threshold) => threshold.ID),
    );
  });

  it("resolves the danger vocabulary from the field rather than from a radius of its own", () => {
    const [hub, fringe, frontier] = world.danger_bands;
    expect(worldField.dangerAt(0, 0).id).toBe(hub!.id);
    expect(worldField.safeAt(0, 0)).toBe(true);
    expect(worldField.safeAt(hub!.outer_radius + 1, 0)).toBe(false);
    expect(worldField.protectedAt(fringe!.outer_radius - 1, 0)).toBe(true);
    expect(worldField.protectedAt(frontier!.outer_radius - 1, 0)).toBe(false);
    // Past the rim resolves to the outermost band rather than to nothing.
    expect(worldField.dangerAt(world.radius * 3, 0).id).toBe(world.danger_bands.at(-1)!.id);
  });

  it("raises the material grade with radius and leaves it alone across biomes", () => {
    expect(worldField.gradeAt(1000, 0).id).toBe("common");
    expect(worldField.gradeAt(10000, 0).id).toBe("uncommon");
    expect(worldField.gradeAt(20000, 0).id).toBe("rare");
    const east = worldField.gradeAt(0, 10000), north = worldField.gradeAt(10000, 0);
    expect(east.id).toBe(north.id);
    expect(east.richness).toBeCloseTo(north.richness, 12);
    expect(east.id).not.toBe(worldField.biomeAt(0, 10000).id);
  });

  it("yields universal stock everywhere and aligned stock only in its own biome", () => {
    for (const biome of Object.keys(biomes)) {
      const yielded = materialsAt(biome, 3);
      for (const id of yielded) {
        const material = materials.materials[id]!;
        const kind = materials.kinds[material.kind]!;
        expect(kind.source).not.toBe("craft");
        if (!kind.universal) expect(material.biome).toBe(biome);
      }
      // Every biome still offers the universal structural stock, which is what
      // "geography never hard-locks a build" means for a crafter.
      expect(yielded).toContain("salvaged-plate");
      // And it offers its own element's stock, which is what makes it worth
      // travelling to.
      expect(yielded.some((id) => materials.materials[id]!.biome === biome)).toBe(true);
    }
  });

  it("treats grade as a ceiling, so walking outward adds options rather than trading them", () => {
    for (const biome of Object.keys(biomes)) {
      const near = materialsAt(biome, 1), far = materialsAt(biome, 3);
      expect(far.length).toBeGreaterThan(near.length);
      for (const id of near) expect(far).toContain(id);
    }
  });
});
