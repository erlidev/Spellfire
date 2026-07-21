import { describe, expect, it } from "vitest";
import { combat, simulation } from "../tuning";
import { Buttons, type Entity } from "../types";
import { Predictor } from "./prediction";

// Derived exactly as prediction.ts derives them, so a table edit moves the
// expectations with the code instead of failing the suite.
const tickRate = simulation.tick_rate;
const { speed } = combat.player;
const { distance: dashDistance, duration_ms: dashDurationMS } = combat.dash;
const dashTicks = Math.max(1, Math.round((dashDurationMS / 1000) * tickRate));
const tickMS = 1000 / tickRate;

function entity(overrides: Partial<Entity> = {}): Entity {
  return {
    type: 1, id: "p", name: "Player", className: "gunslinger", x: 0, y: 0, vx: 0, vy: 0, aimX: 1, aimY: 0,
    health: 100, maxHealth: 100, mana: 10, acknowledgedInput: 0, alive: true, ownerID: "",
    element: "", squadID: "", allegiance: 1, telegraphState: 0, invulnerable: false, telegraphShape: "",
    radius: 0, length: 0, width: 0, angleDegrees: 0, telegraphProgress: 0, abilityID: "", lingering: false, effectIDs: [],
    ...overrides,
  };
}

describe("client prediction", () => {
  it("normalizes diagonal movement", () => {
    const predictor = new Predictor(); predictor.initialize(entity());
    predictor.step(Buttons.Right | Buttons.Down, 1, 0, 0);
    expect(predictor.x).toBeCloseTo(speed / Math.sqrt(2) / tickRate); expect(predictor.y).toBeCloseTo(speed / Math.sqrt(2) / tickRate);
  });

  it("predicts dash only on a press edge", () => {
    const predictor = new Predictor(); predictor.initialize(entity());
    predictor.step(Buttons.Right | Buttons.Dash, 1, 0, 0);
    expect(predictor.x).toBeCloseTo(dashDistance / dashTicks);
    for (let index = 1; index < dashTicks; index++) predictor.step(Buttons.Right | Buttons.Dash, 1, 0, index * tickMS);
    expect(predictor.x).toBeCloseTo(dashDistance);
    predictor.step(Buttons.Right | Buttons.Dash, 1, 0, dashTicks * tickMS);
    expect(predictor.x - dashDistance).toBeCloseTo(speed / tickRate);
  });

  it("carries the dash direction even when movement input changes", () => {
    const predictor = new Predictor(); predictor.initialize(entity());
    predictor.step(Buttons.Right | Buttons.Dash, 1, 0, 0);
    for (let index = 1; index < dashTicks; index++) predictor.step(Buttons.Left, 1, 0, index * tickMS);
    expect(predictor.x).toBeCloseTo(dashDistance); expect(predictor.y).toBeCloseTo(0);
  });

  it("reconciles and replays only unacknowledged motion", () => {
    const predictor = new Predictor(); predictor.initialize(entity());
    predictor.step(Buttons.Right, 1, 0, 0); predictor.step(Buttons.Right, 1, 0, 20);
    predictor.reconcile(entity({ x: 4, acknowledgedInput: 1 }));
    expect(predictor.pendingCount()).toBe(1); expect(predictor.x).toBeCloseTo(4 + speed / tickRate);
  });

  it("uses authoritative tree circles during prediction", () => {
    const predictor = new Predictor(); predictor.initialize(entity()); predictor.setColliders([{ id: "tree", kind: "tree", x: 42, y: 0, radius: 20 }]);
    for (let index = 0; index < 20; index++) predictor.step(Buttons.Right, 1, 0, index * tickMS);
    expect(predictor.x).toBeLessThanOrEqual(42 - 20 - combat.player.radius + 0.01);
  });
});
