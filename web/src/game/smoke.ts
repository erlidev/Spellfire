export interface SmokeCircle { x: number; y: number; radius: number }

export const smokeOffsetScale = 0.4;
export const smokeCenterRadiusScale = 0.62;
export const smokeOuterRadiusScale = 0.6;

/** The five opaque lobes shared by smoke rendering and the LOS shader. */
export function smokeCircles(radius: number): SmokeCircle[] {
  const offset = radius * smokeOffsetScale;
  const outerRadius = radius * smokeOuterRadiusScale;
  return [
    { x: 0, y: 0, radius: radius * smokeCenterRadiusScale },
    { x: offset, y: 0, radius: outerRadius },
    { x: -offset, y: 0, radius: outerRadius },
    { x: 0, y: offset, radius: outerRadius },
    { x: 0, y: -offset, radius: outerRadius },
  ];
}
