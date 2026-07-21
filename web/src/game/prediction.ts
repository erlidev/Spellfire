import { combat, simulation, world } from "../tuning";
import { Buttons, type Collider, type Entity, type InputFrame } from "../types";

// Every constant below is derived from the shared tuning tables, so prediction
// cannot drift from the authoritative simulation through an edited literal.
const tickRate = simulation.tick_rate;
const { radius, speed } = combat.player;
const { distance: dashDistance, duration_ms: dashDurationMS, cooldown_ms: dashCooldownMS } = combat.dash;
// Mirrors the server: the dash covers dashDistance over a whole number of ticks.
const dashTicks = Math.max(1, Math.round((dashDurationMS / 1000) * tickRate));
const dashSpeed = dashDistance / (dashTicks / tickRate);
const worldRadius = world.radius;

interface Motion { x: number; y: number }
interface Pending { input: InputFrame; motion: Motion }

export class Predictor {
  x = 0; y = 0; aimX = 1; aimY = 0;
  private sequence = 0;
  private pending: Pending[] = [];
  private colliders = new Map<string, Collider>();
  private previousButtons = 0;
  private dashReadyAt = 0;
  private dashDirX = 0; private dashDirY = 0;
  private dashTicksLeft = 0;

  setColliders(values: Collider[]): void { for (const value of values) this.colliders.set(value.id, value); }

  initialize(entity: Entity): void { this.x = entity.x; this.y = entity.y; this.aimX = entity.aimX || 1; this.aimY = entity.aimY; this.pending = []; }

  step(buttons: number, aimX: number, aimY: number, selectedSlot: number, now: number): InputFrame {
    const aimLength = Math.hypot(aimX, aimY);
    if (aimLength > 0.001) { this.aimX = aimX / aimLength; this.aimY = aimY / aimLength; }
    let dx = Number(Boolean(buttons & Buttons.Right)) - Number(Boolean(buttons & Buttons.Left));
    let dy = Number(Boolean(buttons & Buttons.Down)) - Number(Boolean(buttons & Buttons.Up));
    const moveLength = Math.hypot(dx, dy);
    if (moveLength) { dx /= moveLength; dy /= moveLength; }
    if ((buttons & Buttons.Dash) && !(this.previousButtons & Buttons.Dash) && now >= this.dashReadyAt) {
      this.dashDirX = moveLength ? dx : this.aimX; this.dashDirY = moveLength ? dy : this.aimY;
      this.dashTicksLeft = dashTicks;
      this.dashReadyAt = now + dashCooldownMS;
    }
    let motion: Motion;
    if (this.dashTicksLeft > 0) {
      motion = { x: this.dashDirX * dashSpeed / tickRate, y: this.dashDirY * dashSpeed / tickRate };
      this.dashTicksLeft--;
    } else {
      motion = { x: dx * speed / tickRate, y: dy * speed / tickRate };
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
    this.x = authoritative.x; this.y = authoritative.y;
    this.pending = this.pending.filter((entry) => entry.input.sequence > authoritative.acknowledgedInput);
    for (const entry of this.pending) this.applyMotion(entry.motion);
  }

  pendingCount(): number { return this.pending.length; }

  private applyMotion(motion: Motion): void {
    let x = this.x + motion.x, y = this.y;
    if (this.collides(x, y)) x = this.x;
    y += motion.y;
    if (this.collides(x, y)) y = this.y;
    const distance = Math.hypot(x, y), limit = worldRadius - radius;
    if (distance > limit) { x *= limit / distance; y *= limit / distance; }
    this.x = x; this.y = y;
  }

  private collides(x: number, y: number): boolean {
    for (const collider of this.colliders.values()) if ((x - collider.x) ** 2 + (y - collider.y) ** 2 < (radius + collider.radius) ** 2) return true;
    return false;
  }
}
