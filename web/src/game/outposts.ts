// Outposts overlay the radial world field on the client exactly as they do on
// the server: the field still answers danger/biome/grade, and the outposts add
// the discoverable no-PvP bubbles and services on top. This is the browser
// mirror of server/internal/game/outpost.go — the two must agree on where a
// player is safe, or the HUD would disagree with the authoritative gate.
import { outposts, world } from "../tuning";
import { worldField } from "./worldfield";

export interface NearbyOutpost { id: string; name: string; distance: number }

function within(x: number, y: number, ox: number, oy: number, radius: number): boolean {
  const dx = x - ox, dy = y - oy;
  return dx * dx + dy * dy <= radius * radius;
}

// insideOutpostBubble reports whether a position sits in any outpost's no-PvP
// radius. The bubble is protected, and a service zone as far as its row allows.
export function insideOutpostBubble(x: number, y: number): boolean {
  for (const outpost of Object.values(outposts)) {
    if (within(x, y, outpost.position[0], outpost.position[1], outpost.safe_radius)) return true;
  }
  return false;
}

// safeAt is the client's overlaid safety answer: the hub or any outpost bubble.
export function safeAt(x: number, y: number): boolean {
  return worldField.safeAt(x, y) || insideOutpostBubble(x, y);
}

// protectedAt reports whether PvP damage is refused: the hub, the restricted
// fringe, or any outpost bubble.
export function protectedAt(x: number, y: number): boolean {
  return worldField.protectedAt(x, y) || insideOutpostBubble(x, y);
}

// serviceAt reports whether a specific service is offered where a body stands:
// the hub offers all of them, an outpost offers what its row declares.
export function serviceAt(x: number, y: number, service: string): boolean {
  if (worldField.safeAt(x, y)) return true;
  for (const outpost of Object.values(outposts)) {
    if (within(x, y, outpost.position[0], outpost.position[1], outpost.safe_radius)) return outpost.services.includes(service);
  }
  return false;
}

// nearestOutpost is the discovered outpost nearest a position, for the HUD and
// the compass. Undiscovered outposts are never surfaced.
export function nearestOutpost(x: number, y: number, discovered: Set<string>): NearbyOutpost | undefined {
  let best: NearbyOutpost | undefined;
  for (const [id, outpost] of Object.entries(outposts)) {
    if (!discovered.has(id)) continue;
    const distance = Math.hypot(x - outpost.position[0], y - outpost.position[1]);
    if (!best || distance < best.distance) best = { id, name: outpost.name, distance };
  }
  return best;
}

// outpostList is the world's outposts, for rendering their markers and rings.
export function outpostList(): { id: string; x: number; y: number; safeRadius: number; name: string }[] {
  return Object.entries(outposts).map(([id, o]) => ({ id, x: o.position[0], y: o.position[1], safeRadius: o.safe_radius, name: o.name }));
}

// worldRadius is re-exported so callers that render outpost markers do not also
// have to import the world table.
export const worldRadius = world.radius;
