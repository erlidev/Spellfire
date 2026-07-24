// The client reads the same versioned tables the server embeds — the files in
// data/tuning are imported here at build time, so a balance edit moves the
// simulation, the prediction constants, and the renderer together. Nothing in
// web/src may re-declare a balance number that exists in a table.
import abilitiesData from "../../data/tuning/abilities.json";
import ammunitionData from "../../data/tuning/ammunition.json";
import biomesData from "../../data/tuning/biomes.json";
import combatData from "../../data/tuning/combat.json";
import effectsData from "../../data/tuning/effects.json";
import componentsData from "../../data/tuning/components.json";
import elementsData from "../../data/tuning/elements.json";
import entitiesData from "../../data/tuning/entities.json";
import gadgetsData from "../../data/tuning/gadgets.json";
import loadoutData from "../../data/tuning/loadout.json";
import manifestData from "../../data/tuning/manifest.json";
import materialsData from "../../data/tuning/materials.json";
import mobsData from "../../data/tuning/mobs.json";
import outpostsData from "../../data/tuning/outposts.json";
import progressionData from "../../data/tuning/progression.json";
import rideablesData from "../../data/tuning/rideables.json";
import sessionData from "../../data/tuning/session.json";
import simulationData from "../../data/tuning/simulation.json";
import spellsData from "../../data/tuning/spells.json";
import weaponsData from "../../data/tuning/weapons.json";
import worldData from "../../data/tuning/world.json";
import type { CharacterClass } from "./types";

export interface Manifest { version: number; schema_version: number }
export interface SessionTable { logout_linger_seconds: number; position_expiry_seconds: number; exit_invuln_seconds: number; mount_lockout_seconds: number }
export interface Outpost { name: string; band: string; position: [number, number]; safe_radius: number; discovery_radius: number; services: string[] }
export interface Rideable { name: string; class: CharacterClass; entity: string; ride_speed: number; cost: Record<string, number> }
export interface Simulation { tick_rate: number; send_rate: number; aoi_radius: number; max_rewind_ms: number; interpolation_delay_ms: number }
export interface DangerBand { id: string; name: string; tier: number; outer_radius: number; material_grade: string; pvp: string; shape: string; summary: string }
export interface CollisionObject { type: "circle" | "box"; offset_x?: number; offset_y?: number; radius?: number; width?: number; height?: number }
export interface AdminOption { value: string; label: string }
export interface AdminField { attribute: string; label: string; input: "number" | "text" | "select" | "position" | "rotation"; scope: "spawn" | "edit" | "both"; default: string; min?: number; max?: number; step?: number; max_length?: number; options?: AdminOption[] }
export interface EntityDefinition { mass: number; max_health: number; occludes_vision: boolean; visible_in_shadow: boolean; collision_objects: CollisionObject[]; admin: { name: string; spawnable: boolean; fields: AdminField[] } }
export interface ScatterArchetype { entity: string; fill: number; radius_spread: number }
export interface BiomeTerrain { barrier: string; scatter: ScatterArchetype[] }
export interface Belts { seed: number; cell: number; thickness: number; radius_spread: number; waviness: number; wave_count: number; passes_per_belt: number; pass_half_angle: number; radii: number[] }
export interface Routes { clear_fill: number; half_angle_scale: number }
export interface Terrain { seed: number; cell: number; inner_margin: number; outer_margin: number; spacing: number; default: BiomeTerrain; biomes: Record<string, BiomeTerrain>; belts: Belts; routes: Routes }
export interface Fixture { id: string; entity: string; position: [number, number] }
export interface GradeThresholdRow { grade: string; at: number }
export interface GradeCurveTable { points: [number, number][]; thresholds: GradeThresholdRow[] }
export interface CoverageRule { resolution: number; minimum_share: number; minimum_samples: number }
export interface WorldFieldTable {
  seed: number; region_cell: number; region_jitter: number; radial_reference: number;
  warp_cell: number; warp_amplitude: number; blend_width: number;
  grade_curve: GradeCurveTable; coverage: CoverageRule;
}
export interface WorldTable { radius: number; spawn_radius: number; chunk_size: number; danger_bands: DangerBand[]; field: WorldFieldTable; terrain: Terrain; fixtures: Fixture[] }
export interface PlayerBody { speed: number; max_mana: number; mana_regen: number }
export interface Dash { distance: number; duration_ms: number; cooldown_ms: number }
export interface DamageBand { name: string; damage_per_hit: number; interval_ms: number; target_ttk_seconds: number; ttk_tolerance_seconds: number }
export interface WeightClass { name: string; movement_multiplier: number; recoil_multiplier: number; move_spread_multiplier: number }
export interface CombatTable { roles: string[]; dodge_vectors: string[]; player: PlayerBody; dash: Dash; weight_classes: Record<string, WeightClass>; damage_bands: Record<string, DamageBand> }
export interface Element { name: string; primary_role: string; secondary: string; character: string }
export interface Homing { turn_degrees_per_second: number; acquire_range: number }
export interface Projectile { kind: string; speed: number; life_seconds: number; radius: number; silhouette: string; pellets?: number; pellet_spread_degrees?: number; hitscan_range?: number; max_range?: number; falloff_start?: number; falloff_min?: number; homing?: Homing }
export interface Cost { kind: "none" | "ammo" | "mana" | "material"; material?: string; amount: number }
export interface Telegraph { shape: "circle" | "cone" | "line" | "ring"; radius?: number; length?: number; width?: number; angle_degrees?: number; active_ms: number; resolved_ms: number }
export interface Blast { radius: number; effects?: string[] }
export interface Guard { arc_degrees: number; movement_multiplier: number; durability: number; regen_per_second: number; regen_delay_ms: number }
export interface Deployable {
  kind: string; radius: number; duration_ms: number; reveal_radius?: number; conceals?: boolean;
  damage_band?: string; damage_fraction?: number; tick_ms?: number; effects?: string[]; final_effects?: string[]; trigger?: boolean;
}
export interface Placement { range: number }
export interface Blink { distance: number }
export interface Chain { jumps: number; range: number }
export interface Cleanse { radius: number; mana_per_effect: number }
export interface Wall { kind: string; segments: number; spacing: number; duration_ms: number }
export interface Ability {
  name: string; cost: Cost; interval_ms: number; cooldown_ms: number; windup_ms?: number; telegraph?: Telegraph;
  dodge_vector?: string; damage_band?: string; projectile?: Projectile; effects?: string[]; requires_scope?: boolean;
  blast?: Blast; guard?: Guard; deployable?: Deployable;
  placement?: Placement; self_effects?: string[]; blink?: Blink; blink_on_hit?: boolean; chain?: Chain; cleanse?: Cleanse; wall?: Wall;
}
export interface Effect { name: string; kind: string; stacking: string; duration_ms: number; tick_ms?: number; damage_band?: string; damage_fraction?: number; speed_multiplier?: number; speed?: number; absorb_hits?: number; damage_multiplier?: number }
export interface Recoil { pattern: number[]; recovery_ms: number }
export interface Spread { standing_degrees: number; moving_degrees: number }
export interface Scope { movement_multiplier: number; spread_multiplier: number; view_bonus: number }
export interface Weapon { name: string; class: CharacterClass; blueprint: string; category: string; starter?: boolean; unlock_level: number; magazine_size?: number; reload_ms?: number; ability?: string; spell?: string; weight?: string; recoil?: Recoil; spread?: Spread; scope?: Scope; cost?: Record<string, number>; requires_craft?: boolean; roles: string[] }
export interface Ammunition { name: string; class: CharacterClass; material: string; count: number; cost: Record<string, number> }
export interface Spell { name: string; element: string; tier: number; starter?: boolean; unlock_level: number; ability: string; roles: string[] }
export interface Gadget { name: string; class: CharacterClass; starter?: boolean; unlock_level: number; ability: string; roles: string[] }
export interface ProgressionTable { max_level: number; base_xp: number; growth: number; sources: Record<string, number>; crafted_item_capacity: number; starter_kit: { unlocks: number }; admin_grant: AdminField }
export interface LoadoutTable { weapon_slots: number; gadget_slots: number; spell_slots: number; affinity: { same_element_per_tier: number } }
export interface Blueprint { name: string; summary: string; slots: string[] }
export interface Component { name: string; blueprint: string; slot: string; kind: "gun_part" | "mana_crystal" | "stave"; tier: number; element?: string; effect: string; cost: Record<string, number>; modifiers: Record<string, number> }
export interface CraftRecipe { blueprint: string; summary: string; slots: Record<string, string[]> }
export interface ComponentsTable { blueprints: Record<string, Blueprint>; components: Record<string, Component>; recipes: Record<string, CraftRecipe> }
export interface Grade { name: string; tier: number; power_multiplier: number }
export interface MaterialKind { name: string; universal: boolean; source: string; summary: string }
export interface Material { name: string; grade: string; kind: string; biome?: string }
export interface MaterialsTable { grades: Record<string, Grade>; kinds: Record<string, MaterialKind>; materials: Record<string, Material>; admin_grant: AdminField }
export interface Mob { name: string; family: string; silhouette: string; damage_band: string; dodge_vector: string; telegraph_shape?: Telegraph["shape"]; turrets: number; behavior: string }
export interface BiomePalette { ground: string; accent: string; haze: string }
export interface Biome { name: string; element: string; summary: string; palette: BiomePalette }
export const manifest = manifestData as Manifest;
export const simulation = simulationData as Simulation;
export const session = sessionData as SessionTable;
export const entityDefinitions = entitiesData as Record<string, EntityDefinition>;
export const world = worldData as unknown as WorldTable;
export const combat = combatData as CombatTable;
export const elements = elementsData as Record<string, Element>;
export const abilities = abilitiesData as Record<string, Ability>;
export const effects = effectsData as Record<string, Effect>;
export const weapons = weaponsData as Record<string, Weapon>;
export const spells = spellsData as Record<string, Spell>;
export const gadgets = gadgetsData as Record<string, Gadget>;
export const ammunition = ammunitionData as Record<string, Ammunition>;
export const outposts = outpostsData as unknown as Record<string, Outpost>;
export const rideables = rideablesData as Record<string, Rideable>;
export const loadoutTable = loadoutData as LoadoutTable;
export const progression = progressionData as ProgressionTable;
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

/** The deterministic first row of a class's basic weapon set. */
export function starterWeapon(characterClass: CharacterClass): Weapon {
  const found = Object.keys(weapons).sort().map((id) => weapons[id]!).find((weapon) => weapon.starter && weapon.class === characterClass);
  if (!found) throw new Error(`No starter weapon for ${characterClass}`);
  return found;
}

/** What the level costs to leave, and zero at the cap — the server's curve. */
export function xpToNext(level: number): number {
  if (level < 1 || level >= progression.max_level) return 0;
  return Math.round(progression.base_xp * progression.growth ** (level - 1));
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

/**
 * The resource the HUD meters: the equipped magazine, the crafted ammunition a
 * weapon spends instead of one, or mana when it has neither.
 */
export function resourceMax(weapon: Weapon | undefined): { label: string; max: number; capped: boolean } {
  if (weapon?.magazine_size) return { label: "Ammo", max: weapon.magazine_size, capped: true };
  const spent = weapon ? specialAmmunition(weapon) : undefined;
  // Crafted ammunition has no magazine to fill: the meter shows what is carried,
  // scaled against one batch so an empty launcher reads as empty.
  if (spent) return { label: materialLabel(spent), max: batchSize(spent), capped: false };
  return { label: "Mana", max: combat.player.max_mana, capped: true };
}

/**
 * How many rounds one build of a material yields, and one when nothing does.
 * Several recipes may yield the same round — the elemental ones are part of
 * what a biome is worth travelling to — so the meter is scaled against the
 * largest batch rather than against whichever row happened to be first.
 */
function batchSize(material: string): number {
  let largest = 0;
  for (const recipe of Object.values(ammunition)) if (recipe.material === material) largest = Math.max(largest, recipe.count);
  return largest || 1;
}

/** The carried material a weapon spends per shot, for a weapon that has no magazine. */
export function specialAmmunition(weapon: Weapon): string | undefined {
  const ability = weapon.ability ? abilities[weapon.ability] : undefined;
  return ability?.cost.kind === "material" ? ability.cost.material : undefined;
}

/**
 * The speed multiplier a ride carries, resolved from the entity archetype the
 * snapshot names. Prediction needs it to step a mounted body at the same speed
 * the server does; a kind that is not a ride scales nothing.
 */
export function rideSpeedFor(entityKind: string): number {
  for (const rideable of Object.values(rideables)) if (rideable.entity === entityKind) return rideable.ride_speed;
  return 1;
}

function materialLabel(id: string): string { return materials.materials[id]?.name ?? id; }

/** The handling class a weapon is balanced on; a staff scales nothing. */
export function weightOf(weapon: Weapon | undefined): WeightClass {
  const weight = weapon?.weight ? combat.weight_classes[weapon.weight] : undefined;
  return weight ?? { name: "", movement_multiplier: 1, recoil_multiplier: 1, move_spread_multiplier: 1 };
}

/**
 * What the equipped kit does to movement: weight class, scope, and a raised
 * shield. Prediction applies exactly this, and so does the server — a mismatch
 * would rubber-band every scoped step.
 */
export function handlingScale(weapon: Weapon | undefined, guard: Guard | undefined, scoped: boolean, guarding: boolean): number {
  let scale = weightOf(weapon).movement_multiplier;
  if (scoped && weapon?.scope) scale *= weapon.scope.movement_multiplier;
  if (guarding && guard) scale *= guard.movement_multiplier;
  return scale;
}

/** What the statuses running on a body do to its movement, for prediction. */
export interface MovementStatus {
  /** The slowest slow acting on the body; slows take the strongest, never compound. */
  scale: number;
  /** A root or a stun: the body may not move or dash under its own power. */
  immobile: boolean;
  /** A stun also suppresses the committed stances and every action. */
  stunned: boolean;
  /** A knockback drives the body from the server; nothing local may predict it. */
  displaced: boolean;
}

export const noMovementStatus: MovementStatus = { scale: 1, immobile: false, stunned: false, displaced: false };

/**
 * What the status layer does to movement, mirroring `World.movementScale`,
 * `rooted`, `stunned`, and `knockback`. Active effect IDs ride the local body's
 * own snapshot, so prediction can apply the same rules the server does instead
 * of walking at full speed and being pulled back by every reconciliation.
 */
export function movementStatus(effectIDs: readonly string[]): MovementStatus {
  const status: MovementStatus = { ...noMovementStatus };
  for (const id of effectIDs) {
    const effect = effects[id];
    if (!effect) continue;
    switch (effect.kind) {
      case "slow": status.scale = Math.min(status.scale, effect.speed_multiplier ?? 1); break;
      case "root": status.immobile = true; break;
      case "stun": status.immobile = true; status.stunned = true; break;
      case "knockback": status.displaced = true; break;
    }
  }
  return status;
}

// Snapshots carry only a projectile's kind, so the renderer resolves its shape
// here. Every projectile belongs to an ability, and kinds are unique across the
// tables, which the server validates at load.
const projectilesByKind = new Map<string, Projectile>();
for (const ability of Object.values(abilities)) if (ability.projectile) projectilesByKind.set(ability.projectile.kind, ability.projectile);

export function projectileByKind(kind: string): Projectile | undefined {
  return projectilesByKind.get(kind);
}

// Deployed fields carry only their archetype on the wire, so what a field is —
// concealing smoke or plainly visible ground — is resolved from the ability
// that places it, exactly as a projectile's silhouette is.
const deployablesByKind = new Map<string, Deployable>();
for (const ability of Object.values(abilities)) if (ability.deployable) deployablesByKind.set(ability.deployable.kind, ability.deployable);

export function deployableByKind(kind: string): Deployable | undefined {
  return deployablesByKind.get(kind);
}
