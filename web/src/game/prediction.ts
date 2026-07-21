import { Buttons, type Collider, type Entity, type InputFrame } from "../types";

const speed = 260;
const radius = 20;
const dashDistance = 105;
const dashCooldownMS = 2200;
// Mirrors the server: the dash covers dashDistance over a whole number of ticks.
const dashTicks = 8;
const dashSpeed = dashDistance / (dashTicks / 60);
const worldRadius = 3000;

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

  step(buttons: number, aimX: number, aimY: number, now: number): InputFrame {
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
      motion = { x: this.dashDirX * dashSpeed / 60, y: this.dashDirY * dashSpeed / 60 };
      this.dashTicksLeft--;
    } else {
      motion = { x: dx * speed / 60, y: dy * speed / 60 };
    }
    this.applyMotion(motion);
    this.previousButtons = buttons;
    const input = { sequence: ++this.sequence, buttons, aimX: this.aimX, aimY: this.aimY, clientTimeMS: Date.now() };
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
