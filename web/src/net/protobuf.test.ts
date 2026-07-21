import { describe, expect, it } from "vitest";
import { decodeServer, encodeInputEnvelope, encodeJoin } from "./protobuf";

describe("protobuf wire codec", () => {
  it("encodes a client input with the schema field numbers", () => {
    const encoded = encodeInputEnvelope({ sequence: 1, buttons: 16, aimX: 1, aimY: 0, clientTimeMS: 0 });
    expect([...encoded]).toEqual([0x08, 0x02, 0x22, 0x09, 0x08, 0x01, 0x10, 0x10, 0x1d, 0x00, 0x00, 0x80, 0x3f]);
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

  it("rejects truncated messages", () => {
    expect(() => decodeServer(Uint8Array.from([0x2a, 0x05, 0x08]).buffer)).toThrow();
  });
});
