// The client reads the same versioned tables the server embeds — the files in
// data/tuning are imported here at build time, so a balance edit moves the
// simulation, the prediction constants, and the renderer together. Nothing in
// web/src may re-declare a balance number that exists in a table.
import abilitiesData from "../../data/tuning/abilities.json";
import adminToolsData from "../../data/tuning/admin_tools.json";
import biomesData from "../../data/tuning/biomes.json";
import combatData from "../../data/tuning/combat.json";
import effectsData from "../../data/tuning/effects.json";
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
export interface Cost { kind: "none" | "ammo" | "mana"; amount: number }
export interface Telegraph { shape: "circle" | "cone" | "line" | "ring"; radius?: number; length?: number; width?: number; angle_degrees?: number; active_ms: number; resolved_ms: number }
export interface Ability { name: string; cost: Cost; interval_ms: number; cooldown_ms: number; windup_ms?: number; telegraph?: Telegraph; dodge_vector?: string; damage_band?: string; projectile?: Projectile; effects?: string[] }
export interface Effect { name: string; kind: string; stacking: string; duration_ms: number; tick_ms?: number; damage_band?: string; damage_fraction?: number; speed_multiplier?: number; speed?: number; absorb_hits?: number }
export interface Weapon { name: string; class: CharacterClass; blueprint: string; category: string; starter?: boolean; magazine_size?: number; reload_ms?: number; ability?: string; spell?: string }
export interface Spell { name: string; element: string; tier: number; starter?: boolean; ability: string }
export interface Blueprint { name: string; slots: string[] }
export interface Component { name: string; blueprint: string; slot: string; effect?: string }
export interface ComponentsTable { blueprints: Record<string, Blueprint>; components: Record<string, Component> }
export interface Grade { name: string; tier: number }
export interface MaterialKind { name: string; universal: boolean; source: string; summary: string }
export interface Material { name: string; grade: string; kind: string; biome?: string }
export interface MaterialsTable { grades: Record<string, Grade>; kinds: Record<string, MaterialKind>; materials: Record<string, Material> }
export interface Mob { name: string; family: string; silhouette: string; damage_band: string; dodge_vector: string; telegraph_shape?: Telegraph["shape"]; turrets: number; behavior: string }
export interface Biome { name: string; element: string }
export interface AdminToolField { id: string; label: string; kind: "number" | "text"; default_text?: string; default_number?: number; minimum?: number; maximum?: number; step?: number; max_length?: number }
export interface AdminSpawnable { name: string; kind: "player" | "projectile" | "telegraph"; class?: CharacterClass; ability?: string; element?: string; fields: AdminToolField[] }
export type AdminAttribute = Omit<AdminToolField, "id">;
export interface AdminTools { spawnables: Record<string, AdminSpawnable>; attributes: Record<string, AdminAttribute> }

export const manifest = manifestData as Manifest;
export const adminTools = adminToolsData as AdminTools;
export const simulation = simulationData as Simulation;
export const session = sessionData as SessionTable;
export const world = worldData as WorldTable;
export const combat = combatData as CombatTable;
export const elements = elementsData as Record<string, Element>;
export const abilities = abilitiesData as Record<string, Ability>;
export const effects = effectsData as Record<string, Effect>;
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

/** What a weapon does when used: its own ability, or its spell's. */
export function abilityFor(weapon: Weapon): Ability {
  const id = weapon.spell ? spells[weapon.spell]?.ability : weapon.ability;
  const ability = id ? abilities[id] : undefined;
  if (!ability) throw new Error(`Weapon ${weapon.name} resolves to no ability`);
  return ability;
}

export function damageOf(band: string): number {
  return damageBand(band).damage_per_hit;
}

/** The shared power band a weapon draws from, through the ability it reaches. */
export function damageBandFor(weapon: Weapon): DamageBand {
  const id = abilityFor(weapon).damage_band;
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
// here. Every projectile belongs to an ability, and kinds are unique across the
// tables, which the server validates at load.
const projectilesByKind = new Map<string, Projectile>();
for (const ability of Object.values(abilities)) if (ability.projectile) projectilesByKind.set(ability.projectile.kind, ability.projectile);

export function projectileByKind(kind: string): Projectile | undefined {
  return projectilesByKind.get(kind);
}
