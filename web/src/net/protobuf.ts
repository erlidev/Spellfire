import type { Collider, Entity, InputFrame, LoadoutSet, ServerMessage } from "../types";

class Writer {
  private bytes: number[] = [];
  tag(field: number, wire: number): void { this.varint(field * 8 + wire); }
  varint(value: number): void {
    let remaining = BigInt(Math.max(0, Math.floor(value)));
    while (remaining > 0x7fn) { this.bytes.push(Number(remaining & 0x7fn) | 0x80); remaining >>= 7n; }
    this.bytes.push(Number(remaining));
  }
  uint(field: number, value: number): void { if (value !== 0) { this.tag(field, 0); this.varint(value); } }
  string(field: number, value: string): void { if (value) this.message(field, new TextEncoder().encode(value)); }
  /** Writes a slot even when it is empty: order is meaning in a positional list. */
  slot(field: number, value: string): void { this.message(field, new TextEncoder().encode(value)); }
  float(field: number, value: number): void {
    if (value === 0) return;
    this.tag(field, 5);
    const data = new ArrayBuffer(4); new DataView(data).setFloat32(0, value, true);
    this.bytes.push(...new Uint8Array(data));
  }
  message(field: number, value: Uint8Array): void { this.tag(field, 2); this.varint(value.length); this.bytes.push(...value); }
  finish(): Uint8Array { return Uint8Array.from(this.bytes); }
}

class Reader {
  pos = 0;
  constructor(readonly bytes: Uint8Array) {}
  get done(): boolean { return this.pos >= this.bytes.length; }
  varint(): number {
    let result = 0n; let shift = 0n;
    while (this.pos < this.bytes.length && shift < 70n) {
      const byte = this.bytes[this.pos++];
      if (byte === undefined) throw new Error("Unexpected end of protobuf message");
      result |= BigInt(byte & 0x7f) << shift;
      if ((byte & 0x80) === 0) return Number(result);
      shift += 7n;
    }
    throw new Error("Invalid protobuf varint");
  }
  fixed32(): number {
    if (this.pos + 4 > this.bytes.length) throw new Error("Unexpected end of float");
    const value = new DataView(this.bytes.buffer, this.bytes.byteOffset + this.pos, 4).getFloat32(0, true);
    this.pos += 4; return value;
  }
  data(): Uint8Array {
    const length = this.varint(); const end = this.pos + length;
    if (end > this.bytes.length) throw new Error("Unexpected end of nested message");
    const value = this.bytes.subarray(this.pos, end); this.pos = end; return value;
  }
  string(): string { return new TextDecoder().decode(this.data()); }
  skip(wire: number): void {
    if (wire === 0) { this.varint(); return; }
    if (wire === 1) { this.pos += 8; return; }
    if (wire === 2) { this.data(); return; }
    if (wire === 5) { this.pos += 4; return; }
    throw new Error(`Unsupported protobuf wire type ${wire}`);
  }
}

function encodeInput(input: InputFrame): Uint8Array {
  const writer = new Writer();
  writer.uint(1, input.sequence); writer.uint(2, input.buttons);
  writer.float(3, input.aimX); writer.float(4, input.aimY); writer.uint(5, input.clientTimeMS);
  writer.uint(6, input.selectedSlot);
  return writer.finish();
}

function encodeLoadout(set: LoadoutSet): Uint8Array {
  const writer = new Writer();
  writer.string(1, set.weapon);
  for (const id of set.gadgets) writer.slot(2, id);
  for (const id of set.spells) writer.slot(3, id);
  return writer.finish();
}

export function encodeLoadoutEnvelope(set: LoadoutSet): Uint8Array {
  const writer = new Writer(); writer.uint(1, 5); writer.message(6, encodeLoadout(set)); return writer.finish();
}

function decodeLoadout(bytes: Uint8Array): LoadoutSet {
  const value: LoadoutSet = { weapon: "", gadgets: [], spells: [] };
  const reader = new Reader(bytes);
  while (!reader.done) {
    const tag = reader.varint(), field = tag >>> 3, wire = tag & 7;
    switch (field) {
      case 1: value.weapon = reader.string(); break;
      case 2: value.gadgets.push(reader.string()); break;
      case 3: value.spells.push(reader.string()); break;
      default: reader.skip(wire);
    }
  }
  return value;
}

export function encodeJoin(token: string, characterID: string): Uint8Array {
  const writer = new Writer(); writer.uint(1, 1); writer.string(2, token); writer.string(3, characterID); return writer.finish();
}

export function encodeInputEnvelope(input: InputFrame): Uint8Array {
  const writer = new Writer(); writer.uint(1, 2); writer.message(4, encodeInput(input)); return writer.finish();
}

export function encodeSimple(kind: 3 | 4, clientTimeMS = 0): Uint8Array {
  const writer = new Writer(); writer.uint(1, kind); writer.uint(5, clientTimeMS); return writer.finish();
}

function decodeEntity(bytes: Uint8Array): Entity {
  const value: Entity = { type: 0, id: "", name: "", className: "", x: 0, y: 0, vx: 0, vy: 0, aimX: 0, aimY: 0, health: 0, maxHealth: 0, mana: 0, acknowledgedInput: 0, alive: false, ownerID: "", element: "", squadID: "", allegiance: 0, telegraphState: 0, invulnerable: false, telegraphShape: "", radius: 0, length: 0, width: 0, angleDegrees: 0, telegraphProgress: 0, abilityID: "", lingering: false, effectIDs: [] };
  const reader = new Reader(bytes);
  while (!reader.done) {
    const tag = reader.varint(), field = tag >>> 3, wire = tag & 7;
    switch (field) {
      case 1: value.type = reader.varint(); break; case 2: value.id = reader.string(); break;
      case 3: value.name = reader.string(); break; case 4: value.className = reader.string(); break;
      case 5: value.x = reader.fixed32(); break; case 6: value.y = reader.fixed32(); break;
      case 7: value.vx = reader.fixed32(); break; case 8: value.vy = reader.fixed32(); break;
      case 9: value.aimX = reader.fixed32(); break; case 10: value.aimY = reader.fixed32(); break;
      case 11: value.health = reader.fixed32(); break; case 12: value.maxHealth = reader.fixed32(); break;
      case 13: value.mana = reader.fixed32(); break; case 14: value.acknowledgedInput = reader.varint(); break;
      case 15: value.alive = reader.varint() !== 0; break; case 16: value.ownerID = reader.string(); break;
      case 17: value.element = reader.string(); break; case 18: value.squadID = reader.string(); break;
      case 19: value.allegiance = reader.varint(); break; case 20: value.telegraphState = reader.varint(); break;
      case 21: value.invulnerable = reader.varint() !== 0; break; case 22: value.telegraphShape = reader.string(); break;
      case 23: value.radius = reader.fixed32(); break; case 24: value.length = reader.fixed32(); break;
      case 25: value.width = reader.fixed32(); break; case 26: value.angleDegrees = reader.fixed32(); break;
      case 27: value.telegraphProgress = reader.fixed32(); break; case 28: value.abilityID = reader.string(); break;
      case 29: value.lingering = reader.varint() !== 0; break; case 30: value.effectIDs.push(reader.string()); break;
      default: reader.skip(wire);
    }
  }
  return value;
}

function decodeCollider(bytes: Uint8Array): Collider {
  const value: Collider = { id: "", x: 0, y: 0, radius: 0, kind: "" };
  const reader = new Reader(bytes);
  while (!reader.done) {
    const tag = reader.varint(), field = tag >>> 3, wire = tag & 7;
    switch (field) {
      case 1: value.id = reader.string(); break; case 2: value.x = reader.fixed32(); break;
      case 3: value.y = reader.fixed32(); break; case 4: value.radius = reader.fixed32(); break;
      case 5: value.kind = reader.string(); break; default: reader.skip(wire);
    }
  }
  return value;
}

export function decodeServer(data: ArrayBuffer): ServerMessage {
  const value: ServerMessage = { kind: 0, serverTick: 0, serverTimeMS: 0, playerID: "", entities: [], colliders: [], error: "", echoedClientTimeMS: 0, loadoutEditable: false, respecOwed: false, level: 0, xp: 0, xpToNext: 0, unlocks: [] };
  const reader = new Reader(new Uint8Array(data));
  while (!reader.done) {
    const tag = reader.varint(), field = tag >>> 3, wire = tag & 7;
    switch (field) {
      case 1: value.kind = reader.varint(); break; case 2: value.serverTick = reader.varint(); break;
      case 3: value.serverTimeMS = reader.varint(); break; case 4: value.playerID = reader.string(); break;
      case 5: value.entities.push(decodeEntity(reader.data())); break; case 6: value.colliders.push(decodeCollider(reader.data())); break;
      case 7: value.error = reader.string(); break; case 8: value.echoedClientTimeMS = reader.varint(); break;
      case 9: value.loadout = decodeLoadout(reader.data()); break;
      case 10: value.loadoutEditable = reader.varint() !== 0; break; case 11: value.respecOwed = reader.varint() !== 0; break;
      case 12: value.level = reader.varint(); break; case 13: value.xp = reader.varint(); break;
      case 14: value.xpToNext = reader.varint(); break; case 15: value.unlocks.push(reader.string()); break;
      default: reader.skip(wire);
    }
  }
  return value;
}
