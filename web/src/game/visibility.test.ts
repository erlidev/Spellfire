import { describe, expect, it } from "vitest";
import type { Collider } from "../types";
import { visibilityPolygon } from "./visibility";

const bounds = { left: -50, right: 50, top: -50, bottom: 50 };
const collider = (shape: "circle" | "box", values: Partial<Collider>): Collider => ({
  id: "cover:0", entityID: "cover", kind: "wall", shape,
  x: 20, y: 0, radius: 0, width: 0, height: 0, ...values,
});

describe("line-of-sight shadow polygon", () => {
  it("leaves the viewport visible when there is no cover", () => {
    const polygon = visibilityPolygon({ x: 0, y: 0 }, bounds, []);
    expect(inside({ x: 40, y: 0 }, polygon)).toBe(true);
    expect(inside({ x: -40, y: 40 }, polygon)).toBe(true);
  });

  it("shadows directly behind box cover without darkening its flanks", () => {
    const polygon = visibilityPolygon({ x: 0, y: 0 }, bounds, [collider("box", { width: 4, height: 24 })]);
    expect(inside({ x: 40, y: 0 }, polygon)).toBe(false);
    expect(inside({ x: 40, y: 35 }, polygon)).toBe(true);
  });

  it("uses circle tangents for trees", () => {
    const polygon = visibilityPolygon({ x: 0, y: 0 }, bounds, [collider("circle", { kind: "tree", radius: 6 })]);
    expect(inside({ x: 40, y: 0 }, polygon)).toBe(false);
    expect(inside({ x: 40, y: 25 }, polygon)).toBe(true);
  });
});

function inside(point: { x: number; y: number }, polygon: { x: number; y: number }[]): boolean {
  let result = false;
  for (let index = 0, previous = polygon.length - 1; index < polygon.length; previous = index++) {
    const a = polygon[index]!, b = polygon[previous]!;
    if ((a.y > point.y) !== (b.y > point.y) && point.x < (b.x - a.x) * (point.y - a.y) / (b.y - a.y) + a.x) result = !result;
  }
  return result;
}
