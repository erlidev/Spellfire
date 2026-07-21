import { TelegraphState } from "../types";

export interface TelegraphStyle { fillAlpha: number; strokeAlpha: number; strokeWidth: number }

// Opacity is the redundant timing channel shared by every telegraph shape.
// Pending warnings intensify toward impact, active areas hold steady, and the
// resolved phase begins with a bright flash before fading away.
export function telegraphStyle(state: number, progress: number): TelegraphStyle {
  const phase = Math.max(0, Math.min(1, progress));
  if (state === TelegraphState.Pending) return { fillAlpha: .1 + .2 * phase, strokeAlpha: .45 + .35 * phase, strokeWidth: 2 };
  if (state === TelegraphState.Active) return { fillAlpha: .42, strokeAlpha: .92, strokeWidth: 3 };
  return { fillAlpha: .5 * (1 - phase), strokeAlpha: 1 - .45 * phase, strokeWidth: 6 - 2 * phase };
}
