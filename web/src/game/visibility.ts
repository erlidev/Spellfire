import type { Collider } from "../types";

export interface Point { x: number; y: number }
export interface Bounds { left: number; right: number; top: number; bottom: number }

const rayEpsilon = 0.00001;

/**
 * Builds the visible polygon from the same circle/box colliders used by the
 * server. Rays skim both sides of every silhouette corner or circle tangent;
 * their nearest intersections form the hole cut out of the shadow overlay.
 */
export function visibilityPolygon(origin: Point, bounds: Bounds, occluders: Collider[]): Point[] {
  const angles: number[] = [];
  const addRay = (angle: number): void => { angles.push(angle - rayEpsilon, angle, angle + rayEpsilon); };
  for (const corner of [
    { x: bounds.left, y: bounds.top }, { x: bounds.right, y: bounds.top },
    { x: bounds.right, y: bounds.bottom }, { x: bounds.left, y: bounds.bottom },
  ]) addRay(Math.atan2(corner.y - origin.y, corner.x - origin.x));

  for (const collider of occluders) {
    if (collider.shape === "circle") {
      const dx = collider.x - origin.x, dy = collider.y - origin.y;
      const distance = Math.hypot(dx, dy);
      if (distance <= collider.radius) continue;
      const center = Math.atan2(dy, dx), tangent = Math.asin(Math.min(1, collider.radius / distance));
      addRay(center - tangent); addRay(center + tangent);
      continue;
    }
    const halfWidth = collider.width / 2, halfHeight = collider.height / 2;
    for (const point of [
      { x: collider.x - halfWidth, y: collider.y - halfHeight },
      { x: collider.x + halfWidth, y: collider.y - halfHeight },
      { x: collider.x + halfWidth, y: collider.y + halfHeight },
      { x: collider.x - halfWidth, y: collider.y + halfHeight },
    ]) addRay(Math.atan2(point.y - origin.y, point.x - origin.x));
  }

  return angles.sort((a, b) => a - b).map((angle) => {
    const direction = { x: Math.cos(angle), y: Math.sin(angle) };
    let distance = rayBounds(origin, direction, bounds);
    for (const collider of occluders) distance = Math.min(distance, rayCollider(origin, direction, collider));
    return { x: origin.x + direction.x * distance, y: origin.y + direction.y * distance };
  });
}

function rayCollider(origin: Point, direction: Point, collider: Collider): number {
  if (collider.shape === "circle") {
    const dx = origin.x - collider.x, dy = origin.y - collider.y;
    const projection = dx * direction.x + dy * direction.y;
    const discriminant = projection * projection - (dx * dx + dy * dy - collider.radius * collider.radius);
    if (discriminant < 0) return Number.POSITIVE_INFINITY;
    const near = -projection - Math.sqrt(discriminant), far = -projection + Math.sqrt(discriminant);
    return near >= 0 ? near : far >= 0 ? far : Number.POSITIVE_INFINITY;
  }
  const halfWidth = collider.width / 2, halfHeight = collider.height / 2;
  let near = Number.NEGATIVE_INFINITY, far = Number.POSITIVE_INFINITY;
  for (const [position, directionAxis, low, high] of [
    [origin.x, direction.x, collider.x - halfWidth, collider.x + halfWidth],
    [origin.y, direction.y, collider.y - halfHeight, collider.y + halfHeight],
  ] as const) {
    if (Math.abs(directionAxis) < 1e-9) {
      if (position < low || position > high) return Number.POSITIVE_INFINITY;
      continue;
    }
    const first = (low - position) / directionAxis, second = (high - position) / directionAxis;
    near = Math.max(near, Math.min(first, second)); far = Math.min(far, Math.max(first, second));
    if (near > far) return Number.POSITIVE_INFINITY;
  }
  return near >= 0 ? near : far >= 0 ? far : Number.POSITIVE_INFINITY;
}

function rayBounds(origin: Point, direction: Point, bounds: Bounds): number {
  let nearest = Number.POSITIVE_INFINITY;
  const consider = (distance: number, cross: number, low: number, high: number): void => {
    if (distance >= 0 && cross >= low - 1e-6 && cross <= high + 1e-6) nearest = Math.min(nearest, distance);
  };
  if (Math.abs(direction.x) > 1e-9) {
    let distance = (bounds.left - origin.x) / direction.x;
    consider(distance, origin.y + direction.y * distance, bounds.top, bounds.bottom);
    distance = (bounds.right - origin.x) / direction.x;
    consider(distance, origin.y + direction.y * distance, bounds.top, bounds.bottom);
  }
  if (Math.abs(direction.y) > 1e-9) {
    let distance = (bounds.top - origin.y) / direction.y;
    consider(distance, origin.x + direction.x * distance, bounds.left, bounds.right);
    distance = (bounds.bottom - origin.y) / direction.y;
    consider(distance, origin.x + direction.x * distance, bounds.left, bounds.right);
  }
  return nearest;
}
