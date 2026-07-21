// The client reads the same versioned tables the server embeds — the files in
// data/tuning are imported here at build time, so a balance edit moves the
// simulation, the prediction constants, and the renderer together. Nothing in
// web/src may re-declare a balance number that exists in a table.
import biomesData from "../../data/tuning/biomes.json";
import combatData from "../../data/tuning/combat.json";
import componentsData from "../../data/tuning/components.json";
import elementsData from "../../data/tuning/elements.json";
import manifestData from "../../data/tuning/manifest.json";
import materialsData from "../../data/tuning/materials.json";
import mobsData from "../../data/tuning/mobs.json";
import sessionData from "../../data/tuning/session.json";
import simulationData from "../../data/tuning/simulation.json";
import spellsData from "../../data/tuning/spells.json";
import weaponsData from "../../data/tuning/weapons.json";
import worldData from "../../data/tuning/world.json";
import type { CharacterClass } from "./types";

export interface Manifest { version: number; schema_version: number }
export interface SessionTable { logout_linger_seconds: number; position_expiry_seconds: number }
export interface Simulation { tick_rate: number; send_rate: number; aoi_radius: number; max_rewind_ms: number; interpolation_delay_ms: number }
export interface DangerBand { id: string; name: string; tier: number; outer_radius: number; material_grade: string; pvp: string; shape: string; summary: string }
export interface Trees { count: number; seed: number; min_radius: number; radius_spread: number; inner_margin: number; outer_margin: number; spacing: number }
export interface WorldTable { radius: number; spawn_radius: number; danger_bands: DangerBand[]; trees: Trees }
export interface PlayerBody { radius: number; speed: number; max_health: number; max_mana: number; mana_regen: number }
export interface Dash { distance: number; duration_ms: number; cooldown_ms: number }
export interface DamageBand { name: string; damage_per_hit: number; target_ttk_seconds: number; ttk_tolerance_seconds: number }
export interface CombatTable { roles: string[]; dodge_vectors: string[]; player: PlayerBody; dash: Dash; damage_bands: Record<string, DamageBand> }
export interface Element { name: string; primary_role: string; secondary: string; character: string }
export interface Projectile { kind: string; speed: number; life_seconds: number; radius: number; silhouette: string }
export interface Weapon { name: string; class: CharacterClass; blueprint: string; category: string; starter?: boolean; damage_band?: string; fire_interval_ms?: number; magazine_size?: number; reload_ms?: number; spell?: string; projectile?: Projectile }
export interface Spell { name: string; element: string; tier: number; starter?: boolean; damage_band: string; mana_cost: number; cast_interval_ms: number; cooldown_ms: number; dodge_vector: string; projectile: Projectile }
export interface Blueprint { name: string; slots: string[] }
export interface Component { name: string; blueprint: string; slot: string; effect?: string }
export interface ComponentsTable { blueprints: Record<string, Blueprint>; components: Record<string, Component> }
export interface Grade { name: string; tier: number }
export interface MaterialKind { name: string; universal: boolean; source: string; summary: string }
export interface Material { name: string; grade: string; kind: string; biome?: string }
export interface MaterialsTable { grades: Record<string, Grade>; kinds: Record<string, MaterialKind>; materials: Record<string, Material> }
export interface Mob { name: string; family: string; silhouette: string; damage_band: string; dodge_vector: string; turrets: number; behavior: string }
export interface Biome { name: string; element: string }

export const manifest = manifestData as Manifest;
export const simulation = simulationData as Simulation;
export const session = sessionData as SessionTable;
export const world = worldData as WorldTable;
export const combat = combatData as CombatTable;
export const elements = elementsData as Record<string, Element>;
export const weapons = weaponsData as Record<string, Weapon>;
export const spells = spellsData as Record<string, Spell>;
export const components = componentsData as ComponentsTable;
export const materials = materialsData as MaterialsTable;
export const mobs = mobsData as Record<string, Mob>;
export const biomes = biomesData as Record<string, Biome>;

/** Outer edge of the fully safe centre, and of PvP protection as a whole. */
export const safeRadius = outerRadiusWhile(["off"]);
export const pvpRadius = outerRadiusWhile(["off", "restricted"]);

function outerRadiusWhile(states: string[]): number {
  let radius = 0;
  for (const band of world.danger_bands) {
    if (!states.includes(band.pvp)) break;
    radius = band.outer_radius;
  }
  return radius;
}

/** Resolves the danger band containing a distance from the world origin. */
export function dangerBandAt(distance: number): DangerBand {
  const last = world.danger_bands[world.danger_bands.length - 1]!;
  return world.danger_bands.find((band) => distance <= band.outer_radius) ?? last;
}

/** The weapon a freshly created character of the class carries. */
export function starterWeapon(characterClass: CharacterClass): Weapon {
  const found = Object.values(weapons).find((weapon) => weapon.starter && weapon.class === characterClass);
  if (!found) throw new Error(`No starter weapon for ${characterClass}`);
  return found;
}

/** A weapon's resolved firing profile, delegating to its spell where it has one. */
export interface Shot { intervalMS: number; damage: number; manaCost: number; projectile: Projectile }

export function shotFor(weapon: Weapon): Shot {
  if (weapon.spell) {
    const spell = spells[weapon.spell];
    if (!spell) throw new Error(`Weapon casts unknown spell ${weapon.spell}`);
    return { intervalMS: spell.cast_interval_ms, damage: damageOf(spell.damage_band), manaCost: spell.mana_cost, projectile: spell.projectile };
  }
  if (!weapon.projectile || !weapon.damage_band || !weapon.fire_interval_ms) throw new Error(`Weapon ${weapon.name} has no resolvable shot`);
  return { intervalMS: weapon.fire_interval_ms, damage: damageOf(weapon.damage_band), manaCost: 0, projectile: weapon.projectile };
}

export function damageOf(band: string): number {
  return damageBand(band).damage_per_hit;
}

/** The shared power band a weapon draws from, through its spell where it casts one. */
export function damageBandFor(weapon: Weapon): DamageBand {
  const id = weapon.spell ? spells[weapon.spell]?.damage_band : weapon.damage_band;
  if (!id) throw new Error(`Weapon ${weapon.name} references no damage band`);
  return damageBand(id);
}

function damageBand(band: string): DamageBand {
  const row = combat.damage_bands[band];
  if (!row) throw new Error(`Unknown damage band ${band}`);
  return row;
}

/** The resource the HUD meters: a magazine for magazine weapons, mana otherwise. */
export function resourceMax(characterClass: CharacterClass): { label: string; max: number } {
  const weapon = starterWeapon(characterClass);
  if (weapon.magazine_size) return { label: "Ammo", max: weapon.magazine_size };
  return { label: "Mana", max: combat.player.max_mana };
}

// Snapshots carry only a projectile's kind, so the renderer resolves its shape
// here. Kinds are unique across the tables, which the server validates at load.
const projectilesByKind = new Map<string, Projectile>();
for (const weapon of Object.values(weapons)) if (weapon.projectile) projectilesByKind.set(weapon.projectile.kind, weapon.projectile);
for (const spell of Object.values(spells)) projectilesByKind.set(spell.projectile.kind, spell.projectile);

export function projectileByKind(kind: string): Projectile | undefined {
  return projectilesByKind.get(kind);
}
