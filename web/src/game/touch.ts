export interface JoystickVector {
  /** Normalized control direction. */
  x: number;
  y: number;
  /** Pixel offset used to position the visible thumb. */
  knobX: number;
  knobY: number;
}

/**
 * Resolves a pointer against a fixed joystick base. The direction reaches full
 * strength at the edge and stays clamped when the thumb drags beyond it.
 */
export function joystickVector(
  clientX: number,
  clientY: number,
  bounds: Pick<DOMRect, "left" | "top" | "width" | "height">,
  travel: number,
): JoystickVector {
  const dx = clientX - (bounds.left + bounds.width / 2);
  const dy = clientY - (bounds.top + bounds.height / 2);
  const distance = Math.hypot(dx, dy);
  const scale = distance > travel && distance > 0 ? travel / distance : 1;
  const knobX = dx * scale;
  const knobY = dy * scale;
  return { x: travel > 0 ? knobX / travel : 0, y: travel > 0 ? knobY / travel : 0, knobX, knobY };
}

/** Digital movement is still the server contract; the stick supplies diagonals. */
export function movementButtons(x: number, y: number, threshold = 0.28): { up: boolean; down: boolean; left: boolean; right: boolean } {
  return { up: y < -threshold, down: y > threshold, left: x < -threshold, right: x > threshold };
}
