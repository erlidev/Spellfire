import { describe, expect, it } from "vitest";
import { decodeServer, encodeInputEnvelope, encodeJoin } from "./protobuf";

describe("protobuf wire codec", () => {
  it("encodes a client input with the schema field numbers", () => {
    const encoded = encodeInputEnvelope({ sequence: 1, buttons: 16, aimX: 1, aimY: 0, selectedSlot: 0, clientTimeMS: 0 });
    expect([...encoded]).toEqual([0x08, 0x02, 0x22, 0x09, 0x08, 0x01, 0x10, 0x10, 0x1d, 0x00, 0x00, 0x80, 0x3f]);
  });

  it("encodes the interact bit without changing the input field", () => {
    const encoded = encodeInputEnvelope({ sequence: 1, buttons: 128, aimX: 0, aimY: 0, selectedSlot: 0, clientTimeMS: 0 });
    expect([...encoded]).toContain(0x80);
    expect([...encoded]).toContain(0x01);
  });

  it("encodes join credentials without JSON", () => {
    const encoded = encodeJoin("token", "hero");
    expect(new TextDecoder().decode(encoded)).toContain("token");
    expect(encoded[0]).toBe(0x08);
  });

  it("decodes a server envelope and nested entity", () => {
    const message = decodeServer(Uint8Array.from([0x08, 0x01, 0x22, 0x04, 0x73, 0x65, 0x6c, 0x66, 0x2a, 0x0c, 0x08, 0x01, 0x12, 0x01, 0x70, 0x2d, 0x00, 0x00, 0xc0, 0x3f, 0x78, 0x01]).buffer);
    expect(message.kind).toBe(1); expect(message.playerID).toBe("self");
    expect(message.entities).toHaveLength(1); expect(message.entities[0]).toMatchObject({ type: 1, id: "p", x: 1.5, alive: true });
  });

  it("decodes the expanded entity and telegraph fields", () => {
    const entity = [
      ...uintField(1, 6), ...stringField(2, "warning"), ...stringField(16, "caster"), ...stringField(17, "fire"),
      ...stringField(18, "squad-a"), ...uintField(19, 4), ...uintField(20, 3), ...uintField(21, 1),
      ...stringField(22, "ring"), ...floatField(23, 120), ...floatField(25, 20), ...floatField(27, .75),
      ...stringField(28, "fire-ring"), ...uintField(29, 1), ...stringField(30, "burn"), ...stringField(30, "slow"),
      ...floatField(31, -1), ...uintField(32, 1), ...floatField(33, .5),
    ];
    const envelope = Uint8Array.from([...uintField(1, 2), ...messageField(5, entity)]).buffer;
    expect(decodeServer(envelope).entities[0]).toMatchObject({
      type: 6, id: "warning", ownerID: "caster", element: "fire", squadID: "squad-a", allegiance: 4,
      telegraphState: 3, invulnerable: true, telegraphShape: "ring", radius: 120, width: 20,
      telegraphProgress: .75, abilityID: "fire-ring", lingering: true, effectIDs: ["burn", "slow"], mass: -1, deleting: true, deleteProgress: .5,
    });
  });

  it("decodes box collision components", () => {
    const collider = [
      ...stringField(1, "wall:0"), ...floatField(2, 10), ...floatField(3, 20), ...stringField(5, "wall"),
      ...stringField(6, "box"), ...floatField(7, 96), ...floatField(8, 96), ...stringField(9, "wall"),
    ];
    const envelope = Uint8Array.from([...uintField(1, 2), ...messageField(6, collider)]).buffer;
    expect(decodeServer(envelope).colliders[0]).toMatchObject({ id: "wall:0", entityID: "wall", shape: "box", x: 10, y: 20, width: 96, height: 96 });
  });

  it("decodes the viewer's own ability lockouts", () => {
    const envelope = Uint8Array.from([
      ...uintField(1, 2),
      ...messageField(18, [...stringField(1, "firestorm-cast"), ...uintField(2, 12_000)]),
      ...messageField(18, [...stringField(1, "ward-cast"), ...uintField(2, 800)]),
    ]).buffer;
    expect(decodeServer(envelope).cooldowns).toEqual({ "firestorm-cast": 12_000, "ward-cast": 800 });
  });

  it("rejects truncated messages", () => {
    expect(() => decodeServer(Uint8Array.from([0x2a, 0x05, 0x08]).buffer)).toThrow();
  });
});

function varint(value: number): number[] {
  const bytes: number[] = [];
  while (value > 0x7f) { bytes.push((value & 0x7f) | 0x80); value >>>= 7; }
  bytes.push(value); return bytes;
}

function tag(field: number, wire: number): number[] { return varint(field * 8 + wire); }
function uintField(field: number, value: number): number[] { return [...tag(field, 0), ...varint(value)]; }
function messageField(field: number, value: number[]): number[] { return [...tag(field, 2), ...varint(value.length), ...value]; }
function stringField(field: number, value: string): number[] { return messageField(field, [...new TextEncoder().encode(value)]); }
function floatField(field: number, value: number): number[] {
  const buffer = new ArrayBuffer(4); new DataView(buffer).setFloat32(0, value, true);
  return [...tag(field, 5), ...new Uint8Array(buffer)];
}
