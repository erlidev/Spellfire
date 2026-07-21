export type CharacterClass = "gunslinger" | "mage";

export interface Character { id: string; name: string; class: CharacterClass; level: number; xp: number }

export interface InputFrame {
  sequence: number;
  buttons: number;
  aimX: number;
  aimY: number;
  clientTimeMS: number;
}

export interface Entity {
  type: number; id: string; name: string; className: string;
  x: number; y: number; vx: number; vy: number; aimX: number; aimY: number;
  health: number; maxHealth: number; mana: number; acknowledgedInput: number;
  alive: boolean; ownerID: string;
}

export interface Collider { id: string; x: number; y: number; radius: number; kind: string }

export interface ServerMessage {
  kind: number; serverTick: number; serverTimeMS: number; playerID: string;
  entities: Entity[]; colliders: Collider[]; error: string; echoedClientTimeMS: number;
}

export const Buttons = { Up: 1, Down: 2, Left: 4, Right: 8, Fire: 16, Dash: 32, Reload: 64 } as const;
export const ClientKind = { Join: 1, Input: 2, Respawn: 3, Ping: 4 } as const;
export const ServerKind = { Welcome: 1, Snapshot: 2, Error: 3, Pong: 4 } as const;
