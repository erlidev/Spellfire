import { describe, expect, it } from "vitest";
import { combat, effects, entityDefinitions, movementStatus, rideSpeedFor, simulation } from "../tuning";
import { Buttons, type Entity } from "../types";
import { Predictor } from "./prediction";

// Derived exactly as prediction.ts derives them, so a table edit moves the
// expectations with the code instead of failing the suite.
const tickRate = simulation.tick_rate;
const { speed } = combat.player;
const { distance: dashDistance, duration_ms: dashDurationMS } = combat.dash;
const dashTicks = Math.max(1, Math.round((dashDurationMS / 1000) * tickRate));
const tickMS = 1000 / tickRate;

// The statuses are looked up by kind rather than named, so retiring a row moves
// the test with the tables instead of breaking it.
function effectOfKind(kind: string): string {
  const id = Object.keys(effects).find((key) => effects[key]!.kind === kind);
  if (!id) throw new Error(`no shipped ${kind} effect`);
  return id;
}
const slowID = effectOfKind("slow");
const slowScale = effects[slowID]!.speed_multiplier!;
// The neutral status, spelled out so a mounted step can be tested without one.
const noStatus = movementStatus([]);

function entity(overrides: Partial<Entity> = {}): Entity {
  return {
    type: 1, id: "p", name: "Player", className: "gunslinger", x: 0, y: 0, vx: 0, vy: 0, aimX: 1, aimY: 0,
    health: 100, maxHealth: 100, mana: 10, acknowledgedInput: 0, alive: true, ownerID: "",
    element: "", squadID: "", allegiance: 1, telegraphState: 0, invulnerable: false, telegraphShape: "",
    radius: 0, length: 0, width: 0, angleDegrees: 0, telegraphProgress: 0, abilityID: "", lingering: false, effectIDs: [],
    mass: 1, deleting: false, deleteProgress: 0, scoped: false, guarding: false, recoilDegrees: 0, shots: 0, shield: 0, maxShield: 0, mounted: false,
    ...overrides,
  };
}

describe("client prediction", () => {
  it("normalizes diagonal movement", () => {
    const predictor = new Predictor(); predictor.initialize(entity());
    predictor.step(Buttons.Right | Buttons.Down, 1, 0, 0, 0);
    expect(predictor.x).toBeCloseTo(speed / Math.sqrt(2) / tickRate); expect(predictor.y).toBeCloseTo(speed / Math.sqrt(2) / tickRate);
  });

  it("predicts dash only on a press edge", () => {
    const predictor = new Predictor(); predictor.initialize(entity());
    predictor.step(Buttons.Right | Buttons.Dash, 1, 0, 0, 0);
    expect(predictor.x).toBeCloseTo(dashDistance / dashTicks);
    for (let index = 1; index < dashTicks; index++) predictor.step(Buttons.Right | Buttons.Dash, 1, 0, 0, index * tickMS);
    expect(predictor.x).toBeCloseTo(dashDistance);
    predictor.step(Buttons.Right | Buttons.Dash, 1, 0, 0, dashTicks * tickMS);
    expect(predictor.x - dashDistance).toBeCloseTo(speed / tickRate);
  });

  it("carries the dash direction even when movement input changes", () => {
    const predictor = new Predictor(); predictor.initialize(entity());
    predictor.step(Buttons.Right | Buttons.Dash, 1, 0, 0, 0);
    for (let index = 1; index < dashTicks; index++) predictor.step(Buttons.Left, 1, 0, 0, index * tickMS);
    expect(predictor.x).toBeCloseTo(dashDistance); expect(predictor.y).toBeCloseTo(0);
  });

  it("reconciles and replays only unacknowledged motion", () => {
    const predictor = new Predictor(); predictor.initialize(entity());
    predictor.step(Buttons.Right, 1, 0, 0, 0); predictor.step(Buttons.Right, 1, 0, 0, 20);
    predictor.reconcile(entity({ x: 4, acknowledgedInput: 1 }));
    expect(predictor.pendingCount()).toBe(1); expect(predictor.x).toBeCloseTo(4 + speed / tickRate);
  });

  it("uses authoritative tree circles during prediction", () => {
    const predictor = new Predictor(); predictor.initialize(entity()); predictor.setColliders([{ id: "tree:0", entityID: "tree", kind: "tree", shape: "circle", x: 42, y: 0, radius: 20, width: 0, height: 0 }]);
    for (let index = 0; index < 20; index++) predictor.step(Buttons.Right, 1, 0, 0, index * tickMS);
    const playerRadius = entityDefinitions.player!.collision_objects[0]!.radius!;
    expect(predictor.x).toBeLessThanOrEqual(42 - 20 - playerRadius + 0.01);
  });

  it("uses authoritative box collision and replaces removed components", () => {
    const predictor = new Predictor(); predictor.initialize(entity());
    predictor.setColliders([{ id: "wall:0", entityID: "wall", kind: "wall", shape: "box", x: 42, y: 0, radius: 0, width: 20, height: 80 }]);
    for (let index = 0; index < 20; index++) predictor.step(Buttons.Right, 1, 0, 0, index * tickMS);
    const blocked = predictor.x;
    predictor.setColliders([]);
    for (let index = 20; index < 40; index++) predictor.step(Buttons.Right, 1, 0, 0, index * tickMS);
    expect(predictor.x).toBeGreaterThan(blocked);
  });

  it("uses an authoritative per-entity radius override", () => {
    const predictor = new Predictor(); predictor.initialize(entity({ radius: 30 }));
    predictor.setColliders([{ id: "tree:0", entityID: "tree", kind: "tree", shape: "circle", x: 60, y: 0, radius: 10, width: 0, height: 0 }]);
    for (let index = 0; index < 20; index++) predictor.step(Buttons.Right, 1, 0, 0, index * tickMS);
    expect(predictor.x).toBeLessThanOrEqual(20.01);
  });

  it("does not predict movement for an immovable entity override", () => {
    const predictor = new Predictor(); predictor.initialize(entity({ mass: -1 }));
    predictor.step(Buttons.Right | Buttons.Dash, 1, 0, 0, 0);
    expect(predictor.x).toBe(0); expect(predictor.y).toBe(0);
  });

  it("predicts a slowed step at the slowed speed", () => {
    const predictor = new Predictor(); predictor.initialize(entity());
    predictor.step(Buttons.Right, 1, 0, 0, 0, 1, movementStatus([slowID]));
    expect(predictor.x).toBeCloseTo(speed * slowScale / tickRate);
  });

  it("takes the strongest slow rather than compounding them", () => {
    const slows = Object.keys(effects).filter((key) => effects[key]!.kind === "slow");
    if (slows.length < 2) return;
    const strongest = Math.min(...slows.map((id) => effects[id]!.speed_multiplier!));
    const predictor = new Predictor(); predictor.initialize(entity());
    predictor.step(Buttons.Right, 1, 0, 0, 0, 1, movementStatus(slows));
    expect(predictor.x).toBeCloseTo(speed * strongest / tickRate);
  });

  it("predicts neither movement nor dash while rooted", () => {
    const predictor = new Predictor(); predictor.initialize(entity());
    predictor.step(Buttons.Right | Buttons.Dash, 1, 0, 0, 0, 1, movementStatus([effectOfKind("root")]));
    expect(predictor.x).toBe(0);
    // The dash was refused rather than merely suppressed, so once the root ends
    // the next press still dashes: nothing was spent on the rooted press.
    predictor.step(0, 1, 0, 0, tickMS);
    predictor.step(Buttons.Right | Buttons.Dash, 1, 0, 0, 2 * tickMS);
    expect(predictor.x).toBeCloseTo(dashDistance / dashTicks);
  });

  it("leaves a knockback entirely to the server and cancels the dash", () => {
    const predictor = new Predictor(); predictor.initialize(entity());
    predictor.step(Buttons.Right | Buttons.Dash, 1, 0, 0, 0);
    const dashed = predictor.x;
    predictor.step(Buttons.Right, 1, 0, 0, tickMS, 1, movementStatus([effectOfKind("knockback")]));
    expect(predictor.x).toBe(dashed);
    // The cancelled dash does not resume once the knockback ends.
    predictor.step(Buttons.Right, 1, 0, 0, 2 * tickMS);
    expect(predictor.x - dashed).toBeCloseTo(speed / tickRate);
  });

  // Riding is server-authoritative movement the client mirrors: the ride's own
  // speed, no dash, and statuses still applying.
  it("predicts a mounted step at the ride's speed and refuses to dash", () => {
    const ride = rideSpeedFor("horse");
    expect(ride).toBeGreaterThan(1);
    const predictor = new Predictor(); predictor.initialize(entity());
    predictor.step(Buttons.Right | Buttons.Dash, 1, 0, 0, 0, 1, noStatus, ride);
    expect(predictor.x).toBeCloseTo(speed * ride / tickRate);
    // The dash was refused rather than spent, so dismounting leaves it ready.
    predictor.step(0, 1, 0, 0, tickMS);
    predictor.step(Buttons.Right | Buttons.Dash, 1, 0, 0, 2 * tickMS);
    expect(predictor.x).toBeCloseTo(speed * ride / tickRate + dashDistance / dashTicks);
  });

  it("still slows a mounted body, so control effects are not shaken off by riding", () => {
    const ride = rideSpeedFor("horse");
    const predictor = new Predictor(); predictor.initialize(entity());
    predictor.step(Buttons.Right, 1, 0, 0, 0, 1, movementStatus([slowID]), ride);
    expect(predictor.x).toBeCloseTo(speed * ride * slowScale / tickRate);
  });
});
