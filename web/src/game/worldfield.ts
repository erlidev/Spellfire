// The browser's copy of the world field. It is a line-for-line mirror of
// server/internal/worldfield/worldfield.go and must stay bit-identical to it:
// the renderer tints the ground by biome and the HUD names the region a player
// is standing in, and a client that disagreed with the server about where a
// border runs would be showing a different world from the one being simulated.
//
// Two rules keep that identity cheap, and both are why this file looks the way
// it does. Every hash is 32-bit integer arithmetic, reproduced here with
// Math.imul and the unsigned shift operators — a 64-bit hash would need BigInt.
// Every float operation is +, -, *, / or Math.sqrt, all of which IEEE-754
// specifies exactly; Math.pow is deliberately absent, because neither language
// is required to round it the same way.
import { biomes, materials, world } from "../tuning";

export interface Band {
  id: string; name: string; pvp: string; material_grade: string; shape: string; summary: string;
  tier: number; outer_radius: number;
}
export interface GradePoint { at: number; richness: number }
export interface GradeThreshold { id: string; tier: number; at: number }
export interface GradeCurve { points: GradePoint[]; thresholds: GradeThreshold[] }

export interface FieldParams {
  radius: number; seed: number; regionCell: number; regionJitter: number;
  radialReference: number; warpCell: number; warpAmplitude: number; blendWidth: number;
  biomeIDs: string[]; bands: Band[]; grades: GradeCurve;
}

/** What covers a position: its region's biome, the nearest different one, and
 *  how far from their shared border it sits — 0 on the border, 1 deep inside. */
export interface BiomeSample { id: string; neighbour: string; blend: number }
/** What the ground is worth: the grade it yields, and the convex richness the
 *  yield scales with inside that grade. */
export interface GradeSample { id: string; tier: number; richness: number }
export interface RegionSample { id: string; danger: Band; biome: BiomeSample; grade: GradeSample }

const SALT_SITE = 0x2545f491;
const SALT_JITTER_X = 0x51ed270b;
const SALT_JITTER_Y = 0x2f9277b5;
const SALT_BIOME = 0x9e3779b1;
const SALT_WARP_X = 0x1b56c4e9;
const SALT_WARP_Y = 0x7f4a7c15;
const SALT_WARP_X2 = 0x27d4eb2f;
const SALT_WARP_Y2 = 0x165667b1;

/** A three-input 32-bit hash. Every step is a 32-bit multiply, xor, or logical
 *  shift, which is exactly the set Go reproduces bit for bit. */
function hash32(a: number, b: number, c: number): number {
  let h = Math.imul(a, 0x9e3779b1) >>> 0;
  h = (h ^ ((b + 0x85ebca6b + ((h << 6) >>> 0) + (h >>> 2)) >>> 0)) >>> 0;
  h = (h ^ ((c + 0xc2b2ae35 + ((h << 6) >>> 0) + (h >>> 2)) >>> 0)) >>> 0;
  h = (h ^ (h >>> 16)) >>> 0;
  h = Math.imul(h, 0x7feb352d) >>> 0;
  h = (h ^ (h >>> 15)) >>> 0;
  h = Math.imul(h, 0x846ca68b) >>> 0;
  return (h ^ (h >>> 16)) >>> 0;
}

/** Maps a hash onto [0,1). The divisor is a power of two, so the division is
 *  exact and needs no rounding agreement between the two languages. */
function unit(h: number): number { return h / 4294967296; }
function index32(value: number): number { return (value | 0) >>> 0; }
function smooth(t: number): number { return t * t * (3 - 2 * t); }
function clamp(value: number, low: number, high: number): number {
  return Math.max(low, Math.min(high, value));
}

export class WorldField {
  private readonly p: FieldParams;

  constructor(params: FieldParams) {
    this.p = { ...params, biomeIDs: [...params.biomeIDs].sort() };
  }

  /** The band containing a position. Anything past the rim resolves to the
   *  outermost band rather than to nothing. */
  dangerAt(x: number, y: number): Band {
    const distance = Math.sqrt(x * x + y * y);
    return this.p.bands.find((band) => distance <= band.outer_radius) ?? this.p.bands[this.p.bands.length - 1]!;
  }

  protectedAt(x: number, y: number): boolean {
    const pvp = this.dangerAt(x, y).pvp;
    return pvp === "off" || pvp === "restricted";
  }

  safeAt(x: number, y: number): boolean { return this.dangerAt(x, y).pvp === "off"; }

  /** The convex reward curve, in [0,1]. It rises slowly across the Fringe and
   *  steeply toward the Deadlands, which is what makes the middle bands a route
   *  rather than the best farm. */
  richnessAt(x: number, y: number): number {
    const points = this.p.grades.points;
    if (points.length === 0 || this.p.radius <= 0) return 0;
    const at = Math.sqrt(x * x + y * y) / this.p.radius;
    if (at <= points[0]!.at) return points[0]!.richness;
    for (let index = 1; index < points.length; index++) {
      const previous = points[index - 1]!, current = points[index]!;
      if (at > current.at) continue;
      const span = current.at - previous.at;
      if (span <= 0) return current.richness;
      return previous.richness + (current.richness - previous.richness) * (at - previous.at) / span;
    }
    return points[points.length - 1]!.richness;
  }

  gradeAt(x: number, y: number): GradeSample {
    const richness = this.richnessAt(x, y);
    const grade: GradeSample = { id: "", tier: 0, richness };
    for (const threshold of this.p.grades.thresholds) {
      if (richness >= threshold.at) { grade.id = threshold.id; grade.tier = threshold.tier; }
    }
    return grade;
  }

  biomeAt(x: number, y: number): BiomeSample { return this.region(x, y).biome; }

  regionAt(x: number, y: number): RegionSample {
    const { cellX, cellY, biome } = this.region(x, y);
    return {
      id: `region-${cellX}:${cellY}`,
      danger: this.dangerAt(x, y),
      biome,
      grade: this.gradeAt(x, y),
    };
  }

  /**
   * Resolves the owning lattice site and the border blend around it.
   *
   * The search is five cells wide rather than three: a site may sit anywhere
   * inside its own cell, so the nearest site to a point at a cell corner can
   * still be two cells away on an axis, and a region border that flickers is
   * worse than one that costs twenty-five hashes.
   */
  private region(x: number, y: number): { cellX: number; cellY: number; biome: BiomeSample } {
    if (this.p.biomeIDs.length === 0 || this.p.regionCell <= 0) {
      return { cellX: 0, cellY: 0, biome: { id: "", neighbour: "", blend: 0 } };
    }
    const [cx, cy] = this.compress(x, y);
    const [wx, wy] = this.warp(cx, cy);
    const baseX = Math.floor(wx / this.p.regionCell), baseY = Math.floor(wy / this.p.regionCell);
    let nearest = Infinity, other = Infinity;
    let ownerX = 0, ownerY = 0, ownerBiome = "", neighbour = "";
    for (let dy = -2; dy <= 2; dy++) {
      for (let dx = -2; dx <= 2; dx++) {
        const [site, biome] = this.site(baseX + dx, baseY + dy);
        const distance = Math.sqrt((site[0] - wx) * (site[0] - wx) + (site[1] - wy) * (site[1] - wy));
        if (distance < nearest) { nearest = distance; ownerX = baseX + dx; ownerY = baseY + dy; ownerBiome = biome; }
      }
    }
    for (let dy = -2; dy <= 2; dy++) {
      for (let dx = -2; dx <= 2; dx++) {
        const [site, biome] = this.site(baseX + dx, baseY + dy);
        if (biome === ownerBiome) continue;
        const distance = Math.sqrt((site[0] - wx) * (site[0] - wx) + (site[1] - wy) * (site[1] - wy));
        if (distance < other) { other = distance; neighbour = biome; }
      }
    }
    let blend = 1;
    const width = this.p.blendWidth * this.p.regionCell;
    if (width > 0 && Number.isFinite(other)) blend = clamp((other - nearest) / width, 0, 1);
    return { cellX: ownerX, cellY: ownerY, biome: { id: ownerBiome, neighbour, blend } };
  }

  /** One lattice site: its jittered position and the biome it owns, both drawn
   *  from the seed and the site's own coordinates alone. */
  private site(cellX: number, cellY: number): [[number, number], string] {
    const ix = index32(cellX), iy = index32(cellY);
    const base = hash32((this.p.seed ^ SALT_SITE) >>> 0, ix, iy);
    const jitter = clamp(this.p.regionJitter, 0, 1);
    const site: [number, number] = [
      (cellX + 0.5 + (unit(hash32(base, ix, SALT_JITTER_X)) - 0.5) * jitter) * this.p.regionCell,
      (cellY + 0.5 + (unit(hash32(base, iy, SALT_JITTER_Y)) - 0.5) * jitter) * this.p.regionCell,
    ];
    let index = Math.trunc(unit(hash32(base, SALT_BIOME, 0)) * this.p.biomeIDs.length);
    if (index >= this.p.biomeIDs.length) index = this.p.biomeIDs.length - 1;
    return [site, this.p.biomeIDs[index]!];
  }

  /**
   * Pulls a position toward the reference radius by a fixed 3/4 power, written
   * as two square roots because that is the only way Go and JavaScript are
   * guaranteed to agree on it to the bit.
   *
   * The effect is why biome and grade are independent axes: a region becomes a
   * swathe that narrows toward the hub and widens toward the rim, so every
   * element is reachable from every danger band.
   */
  private compress(x: number, y: number): [number, number] {
    const distance = Math.sqrt(x * x + y * y);
    if (distance < 1 || this.p.radialReference <= 0) return [x, y];
    const ratio = this.p.radialReference / distance;
    const scale = Math.sqrt(ratio * Math.sqrt(ratio));
    return [x * scale, y * scale];
  }

  /** Bends the lattice with two octaves of value noise, so region borders read
   *  as coastlines rather than as straight Voronoi seams. */
  private warp(x: number, y: number): [number, number] {
    if (this.p.warpCell <= 0 || this.p.warpAmplitude === 0) return [x, y];
    const u = x / this.p.warpCell, v = y / this.p.warpCell;
    const dx = this.noise(SALT_WARP_X, u, v) + 0.5 * this.noise(SALT_WARP_X2, u * 2, v * 2);
    const dy = this.noise(SALT_WARP_Y, u, v) + 0.5 * this.noise(SALT_WARP_Y2, u * 2, v * 2);
    return [x + this.p.warpAmplitude * dx, y + this.p.warpAmplitude * dy];
  }

  /** Bilinear value noise in [-1,1], smoothstepped so the warp has no
   *  lattice-aligned creases in it. */
  private noise(salt: number, x: number, y: number): number {
    const gx = Math.floor(x), gy = Math.floor(y);
    const sx = smooth(x - gx), sy = smooth(y - gy);
    const ix = index32(gx), iy = index32(gy);
    const seed = (this.p.seed ^ salt) >>> 0;
    const n00 = unit(hash32(seed, ix, iy)), n10 = unit(hash32(seed, (ix + 1) >>> 0, iy));
    const n01 = unit(hash32(seed, ix, (iy + 1) >>> 0)), n11 = unit(hash32(seed, (ix + 1) >>> 0, (iy + 1) >>> 0));
    const top = n00 + (n10 - n00) * sx;
    const bottom = n01 + (n11 - n01) * sx;
    return (top + (bottom - top) * sy) * 2 - 1;
  }
}

/** Projects the shipped tables onto the field's inputs, mirroring
 *  `Tables.FieldParams` so both languages build the same field from the same
 *  rows rather than from two hand-kept copies. */
export function fieldParams(): FieldParams {
  const field = world.field;
  return {
    radius: world.radius,
    seed: field.seed >>> 0,
    regionCell: field.region_cell,
    regionJitter: field.region_jitter,
    radialReference: field.radial_reference,
    warpCell: field.warp_cell,
    warpAmplitude: field.warp_amplitude,
    blendWidth: field.blend_width,
    biomeIDs: Object.keys(biomes).sort(),
    bands: world.danger_bands,
    grades: {
      points: field.grade_curve.points.map(([at, richness]) => ({ at, richness })),
      thresholds: field.grade_curve.thresholds.map((threshold) => ({
        id: threshold.grade,
        tier: materials.grades[threshold.grade]?.tier ?? 0,
        at: threshold.at,
      })),
    },
  };
}

/** The shipped world field. One instance: it is stateless and pure. */
export const worldField = new WorldField(fieldParams());

/** A biome's ambient palette as renderer colours. An unknown biome — which is
 *  only reachable from a field with no biomes at all — falls back to a neutral
 *  slate rather than to black, so a broken table is legible instead of invisible. */
export function biomePalette(id: string): { ground: number; accent: number; haze: number } {
  const palette = biomes[id]?.palette;
  return {
    ground: hexColour(palette?.ground, 0x16233a),
    accent: hexColour(palette?.accent, 0x2c405e),
    haze: hexColour(palette?.haze, 0x7ee1bb),
  };
}

function hexColour(value: string | undefined, fallback: number): number {
  if (!value || value.length !== 7 || value[0] !== "#") return fallback;
  const parsed = Number.parseInt(value.slice(1), 16);
  return Number.isNaN(parsed) ? fallback : parsed;
}

/** Blends two packed colours, `t` of the way from `from` to `to`. */
export function mixColour(from: number, to: number, t: number): number {
  const clamped = clamp(t, 0, 1);
  const channel = (shift: number): number =>
    Math.round(((from >> shift) & 0xff) + (((to >> shift) & 0xff) - ((from >> shift) & 0xff)) * clamped);
  return (channel(16) << 16) | (channel(8) << 8) | channel(0);
}

/** The biome's display name, for the HUD readout. */
export function biomeName(id: string): string { return biomes[id]?.name ?? id; }

/** The material grade's display name. */
export function gradeName(id: string): string { return materials.grades[id]?.name ?? id; }

/**
 * What ground of a biome and grade tier can yield, mirroring
 * `Tables.MaterialsAt`: universal stock everywhere, the biome's own aligned
 * rows, and grade as a ceiling rather than an equality, so walking outward adds
 * options instead of trading them.
 */
export function materialsAt(biome: string, tier: number): string[] {
  const found: string[] = [];
  for (const [id, material] of Object.entries(materials.materials)) {
    const kind = materials.kinds[material.kind];
    if (!kind || kind.source === "craft") continue;
    const grade = materials.grades[material.grade];
    if (!grade || grade.tier > tier) continue;
    if (!kind.universal && material.biome !== biome) continue;
    found.push(id);
  }
  return found.sort();
}
