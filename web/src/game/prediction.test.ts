import { describe, expect, it } from "vitest";
import { Buttons, type Entity } from "../types";
import { Predictor } from "./prediction";

function entity(overrides: Partial<Entity> = {}): Entity {
  return { type: 1, id: "p", name: "Player", className: "gunslinger", x: 0, y: 0, vx: 0, vy: 0, aimX: 1, aimY: 0, health: 100, maxHealth: 100, mana: 10, acknowledgedInput: 0, alive: true, ownerID: "", ...overrides };
}

describe("client prediction", () => {
  it("normalizes diagonal movement", () => {
    const predictor = new Predictor(); predictor.initialize(entity());
    predictor.step(Buttons.Right | Buttons.Down, 1, 0, 0);
    expect(predictor.x).toBeCloseTo(260 / Math.sqrt(2) / 60); expect(predictor.y).toBeCloseTo(260 / Math.sqrt(2) / 60);
  });

  it("predicts dash only on a press edge", () => {
    const predictor = new Predictor(); predictor.initialize(entity());
    predictor.step(Buttons.Right | Buttons.Dash, 1, 0, 0);
    expect(predictor.x).toBeCloseTo(105 / 8);
    for (let index = 1; index < 8; index++) predictor.step(Buttons.Right | Buttons.Dash, 1, 0, index * 17);
    expect(predictor.x).toBeCloseTo(105);
    predictor.step(Buttons.Right | Buttons.Dash, 1, 0, 8 * 17);
    expect(predictor.x - 105).toBeCloseTo(260 / 60);
  });

  it("carries the dash direction even when movement input changes", () => {
    const predictor = new Predictor(); predictor.initialize(entity());
    predictor.step(Buttons.Right | Buttons.Dash, 1, 0, 0);
    for (let index = 1; index < 8; index++) predictor.step(Buttons.Left, 1, 0, index * 17);
    expect(predictor.x).toBeCloseTo(105); expect(predictor.y).toBeCloseTo(0);
  });

  it("reconciles and replays only unacknowledged motion", () => {
    const predictor = new Predictor(); predictor.initialize(entity());
    predictor.step(Buttons.Right, 1, 0, 0); predictor.step(Buttons.Right, 1, 0, 20);
    predictor.reconcile(entity({ x: 4, acknowledgedInput: 1 }));
    expect(predictor.pendingCount()).toBe(1); expect(predictor.x).toBeCloseTo(4 + 260 / 60);
  });

  it("uses authoritative tree circles during prediction", () => {
    const predictor = new Predictor(); predictor.initialize(entity()); predictor.setColliders([{ id: "tree", kind: "tree", x: 42, y: 0, radius: 20 }]);
    for (let index = 0; index < 20; index++) predictor.step(Buttons.Right, 1, 0, index * 17);
    expect(predictor.x).toBeLessThanOrEqual(4.34);
  });
});
