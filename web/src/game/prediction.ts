import { combat, entityDefinitions, simulation, world } from "../tuning";
import { Buttons, type Collider, type Entity, type InputFrame } from "../types";

// Every constant below is derived from the shared tuning tables, so prediction
// cannot drift from the authoritative simulation through an edited literal.
const tickRate = simulation.tick_rate;
const { speed } = combat.player;
const defaultRadius = entityDefinitions.player!.collision_objects.find((object) => object.type === "circle")!.radius!;
const { distance: dashDistance, duration_ms: dashDurationMS, cooldown_ms: dashCooldownMS } = combat.dash;
// Mirrors the server: the dash covers dashDistance over a whole number of ticks.
const dashTicks = Math.max(1, Math.round((dashDurationMS / 1000) * tickRate));
const dashSpeed = dashDistance / (dashTicks / tickRate);
const worldRadius = world.radius;

interface Motion { x: number; y: number }
interface Pending { input: InputFrame; motion: Motion }

export class Predictor {
  x = 0; y = 0; aimX = 1; aimY = 0;
  private radius = defaultRadius;
  private mass = 1;
  private sequence = 0;
  private pending: Pending[] = [];
  private colliders = new Map<string, Collider>();
  private previousButtons = 0;
  private dashReadyAt = 0;
  private dashDirX = 0; private dashDirY = 0;
  private dashTicksLeft = 0;

  setColliders(values: Collider[]): void { this.colliders = new Map(values.map((value) => [value.id, value])); }

  initialize(entity: Entity): void { this.x = entity.x; this.y = entity.y; this.aimX = entity.aimX || 1; this.aimY = entity.aimY; this.radius = entity.radius || defaultRadius; this.mass = entity.mass; this.pending = []; }

  /**
   * Advances one predicted tick. `handling` is what the equipped kit does to
   * movement — weight class, scope, raised shield — and is passed in rather than
   * derived here, because prediction has no view of the loadout. The server
   * applies exactly the same multiplier, so a scoped step does not rubber-band.
   */
  step(buttons: number, aimX: number, aimY: number, selectedSlot: number, now: number, handling = 1): InputFrame {
    const aimLength = Math.hypot(aimX, aimY);
    if (aimLength > 0.001) { this.aimX = aimX / aimLength; this.aimY = aimY / aimLength; }
    let dx = Number(Boolean(buttons & Buttons.Right)) - Number(Boolean(buttons & Buttons.Left));
    let dy = Number(Boolean(buttons & Buttons.Down)) - Number(Boolean(buttons & Buttons.Up));
    const moveLength = Math.hypot(dx, dy);
    if (moveLength) { dx /= moveLength; dy /= moveLength; }
    if ((buttons & Buttons.Dash) && !(this.previousButtons & Buttons.Dash) && now >= this.dashReadyAt && this.mass >= 0) {
      this.dashDirX = moveLength ? dx : this.aimX; this.dashDirY = moveLength ? dy : this.aimY;
      this.dashTicksLeft = dashTicks;
      this.dashReadyAt = now + dashCooldownMS;
    }
    let motion: Motion;
    if (this.mass < 0) {
      motion = { x: 0, y: 0 }; this.dashTicksLeft = 0;
    } else if (this.dashTicksLeft > 0) {
      motion = { x: this.dashDirX * dashSpeed / tickRate, y: this.dashDirY * dashSpeed / tickRate };
      this.dashTicksLeft--;
    } else {
      motion = { x: dx * speed * handling / tickRate, y: dy * speed * handling / tickRate };
    }
    this.applyMotion(motion);
    this.previousButtons = buttons;
    // The selected slot is carried, not predicted: abilities resolve on the
    // server, so nothing here re-simulates what the slot does.
    const input = { sequence: ++this.sequence, buttons, aimX: this.aimX, aimY: this.aimY, selectedSlot, clientTimeMS: Date.now() };
    this.pending.push({ input, motion });
    if (this.pending.length > 240) this.pending.shift();
    return input;
  }

  reconcile(authoritative: Entity): void {
    this.x = authoritative.x; this.y = authoritative.y; this.radius = authoritative.radius || defaultRadius; this.mass = authoritative.mass;
    this.pending = this.pending.filter((entry) => entry.input.sequence > authoritative.acknowledgedInput);
    for (const entry of this.pending) this.applyMotion(entry.motion);
  }

  pendingCount(): number { return this.pending.length; }

  private applyMotion(motion: Motion): void {
    let x = this.x + motion.x, y = this.y;
    if (this.collides(x, y)) x = this.x;
    y += motion.y;
    if (this.collides(x, y)) y = this.y;
    const distance = Math.hypot(x, y), limit = worldRadius - this.radius;
    if (distance > limit) { x *= limit / distance; y *= limit / distance; }
    this.x = x; this.y = y;
  }

  private collides(x: number, y: number): boolean {
    for (const collider of this.colliders.values()) {
      if (collider.shape === "box") {
        const dx = Math.max(Math.abs(x - collider.x) - collider.width / 2, 0);
        const dy = Math.max(Math.abs(y - collider.y) - collider.height / 2, 0);
        if (dx ** 2 + dy ** 2 < this.radius ** 2) return true;
      } else if ((x - collider.x) ** 2 + (y - collider.y) ** 2 < (this.radius + collider.radius) ** 2) return true;
    }
    return false;
  }
}
