import { describe, expect, it } from "vitest";
import type { Collider } from "../types";
import { packShadowOccluders } from "./shadow";

const collider = (id: string, shape: "circle" | "box", x: number): Collider => ({
  id, entityID: id, kind: shape === "circle" ? "stone-wall" : "wall", shape,
  x, y: 10, radius: 6, width: 20, height: 12,
});

describe("sight shadow GPU input", () => {
  it("packs circles and boxes into their analytic shader shapes", () => {
    const packed = packShadowOccluders([collider("circle", "circle", 10), collider("box", "box", 20)], { x: 0, y: 0 });
    expect(packed.count).toBe(2);
    expect([...packed.data.slice(0, 4)]).toEqual([10, 10, 6, -1]);
    expect([...packed.data.slice(4, 8)]).toEqual([20, 10, 10, 6]);
  });

  it("keeps the nearest 32 occluders in the bounded uniform block", () => {
    const colliders = Array.from({ length: 40 }, (_, index) => collider(String(index), "box", 100 - index));
    const packed = packShadowOccluders(colliders, { x: 0, y: 0 });
    expect(packed.count).toBe(32);
    expect(packed.data[0]).toBe(61);
    expect(packed.data.length).toBe(32 * 4);
  });
});
