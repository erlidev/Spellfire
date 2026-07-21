export type CharacterClass = "gunslinger" | "mage";

/** `unlocks` is the flat permanent ledger: every owned weapon, spell, and gadget ID. */
export interface Character { id: string; name: string; class: CharacterClass; level: number; xp: number; unlocks: string[] }

export interface InputFrame {
  sequence: number;
  buttons: number;
  aimX: number;
  aimY: number;
  /** Zero-based action-bar slot the use button acts through. */
  selectedSlot: number;
  clientTimeMS: number;
}

/** The equipped set. Both arrays are positional; an empty slot is "". */
export interface LoadoutSet { weapon: string; gadgets: string[]; spells: string[] }

export interface Entity {
  type: number; id: string; name: string; className: string;
  x: number; y: number; vx: number; vy: number; aimX: number; aimY: number;
  health: number; maxHealth: number; mana: number; acknowledgedInput: number;
  alive: boolean; ownerID: string; element: string; squadID: string; allegiance: number;
  telegraphState: number; invulnerable: boolean; telegraphShape: string;
  radius: number; length: number; width: number; angleDegrees: number; telegraphProgress: number;
  abilityID: string; lingering: boolean; effectIDs: string[];
  mass: number;
}

export interface Collider {
  id: string; entityID: string; x: number; y: number; radius: number;
  width: number; height: number; kind: string; shape: "circle" | "box";
}

export interface ServerMessage {
  kind: number; serverTick: number; serverTimeMS: number; playerID: string;
  entities: Entity[]; colliders: Collider[]; error: string; echoedClientTimeMS: number;
  /** Present on Welcome and Loadout replies only, never on a snapshot. */
  loadout?: LoadoutSet; loadoutEditable: boolean; respecOwed: boolean;
  /** The permanent axis, on Welcome and Progress only. `xpToNext` is derived from the curve. */
  level: number; xp: number; xpToNext: number; unlocks: string[];
}

export const Buttons = { Up: 1, Down: 2, Left: 4, Right: 8, Fire: 16, Dash: 32, Reload: 64, Interact: 128 } as const;
export const EntityType = { Player: 1, Projectile: 2, Mob: 3, Drop: 4, Node: 5, Telegraph: 6, Deployable: 7, Boss: 8, WorldItem: 9 } as const;
export const Allegiance = { Self: 1, Squad: 2, Neutral: 3, Hostile: 4 } as const;
export const TelegraphState = { Pending: 1, Active: 2, Resolved: 3 } as const;
export const ClientKind = { Join: 1, Input: 2, Respawn: 3, Ping: 4, Loadout: 5 } as const;
export const ServerKind = { Welcome: 1, Snapshot: 2, Error: 3, Pong: 4, Loadout: 5, Progress: 6 } as const;
