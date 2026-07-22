import { describe, expect, it } from "vitest";
import { smokeCircles } from "./smoke";

describe("smoke geometry", () => {
  it("uses five overlapping lobes bounded by the authored radius", () => {
    const circles = smokeCircles(100);
    expect(circles).toHaveLength(5);
    expect(circles[0]).toEqual({ x: 0, y: 0, radius: 62 });
    for (const circle of circles) {
      expect(Math.hypot(circle.x, circle.y) + circle.radius).toBeLessThanOrEqual(100);
    }
  });
});
