import { describe, expect, it } from "vitest";
import { TelegraphState } from "../types";
import { telegraphStyle } from "./telegraph";

describe("shared telegraph visual grammar", () => {
  it("intensifies pending warnings toward impact", () => {
    const early = telegraphStyle(TelegraphState.Pending, 0);
    const late = telegraphStyle(TelegraphState.Pending, 1);
    expect(late.fillAlpha).toBeGreaterThan(early.fillAlpha);
    expect(late.strokeAlpha).toBeGreaterThan(early.strokeAlpha);
  });

  it("uses a distinct active state and a fading resolution flash", () => {
    const active = telegraphStyle(TelegraphState.Active, .5);
    const flash = telegraphStyle(TelegraphState.Resolved, 0);
    const faded = telegraphStyle(TelegraphState.Resolved, 1);
    expect(active.strokeWidth).toBe(3);
    expect(flash.strokeWidth).toBeGreaterThan(active.strokeWidth);
    expect(flash.fillAlpha).toBeGreaterThan(faded.fillAlpha);
    expect(flash.strokeAlpha).toBeGreaterThan(faded.strokeAlpha);
  });

  it("clamps progress supplied by interpolation", () => {
    expect(telegraphStyle(TelegraphState.Pending, -1)).toEqual(telegraphStyle(TelegraphState.Pending, 0));
    expect(telegraphStyle(TelegraphState.Resolved, 2)).toEqual(telegraphStyle(TelegraphState.Resolved, 1));
  });
});
