import { describe, expect, it } from "vitest";
import { joystickVector, movementButtons } from "./touch";

const bounds = { left: 10, top: 20, width: 100, height: 100 };

describe("fixed joystick", () => {
  it("uses the fixed center and clamps its thumb to the travel radius", () => {
    expect(joystickVector(60, 70, bounds, 40)).toEqual({ x: 0, y: 0, knobX: 0, knobY: 0 });
    expect(joystickVector(160, 70, bounds, 40)).toEqual({ x: 1, y: 0, knobX: 40, knobY: 0 });
  });

  it("preserves diagonals and applies a neutral dead zone", () => {
    const diagonal = joystickVector(100, 110, bounds, 40);
    expect(diagonal.x).toBeCloseTo(Math.SQRT1_2);
    expect(diagonal.y).toBeCloseTo(Math.SQRT1_2);
    expect(movementButtons(diagonal.x, diagonal.y)).toEqual({ up: false, down: true, left: false, right: true });
    expect(movementButtons(0.2, -0.2)).toEqual({ up: false, down: false, left: false, right: false });
  });
});
