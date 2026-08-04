import { describe, expect, it } from "vitest";
import { isDragGesture } from "./pointerDrag";

describe("isDragGesture", () => {
  it("is false when the pointer hasn't moved", () => {
    expect(isDragGesture({ x: 0, y: 0 }, 4)).toBe(false);
  });

  it("is false just below the threshold", () => {
    expect(isDragGesture({ x: 3, y: 0 }, 4)).toBe(false);
  });

  it("is true exactly at the threshold", () => {
    expect(isDragGesture({ x: 4, y: 0 }, 4)).toBe(true);
  });

  it("is true above the threshold", () => {
    expect(isDragGesture({ x: 10, y: 0 }, 4)).toBe(true);
  });

  it("checks the y axis independently of x", () => {
    expect(isDragGesture({ x: 0, y: 4 }, 4)).toBe(true);
    expect(isDragGesture({ x: 0, y: 3 }, 4)).toBe(false);
  });

  it("uses the larger of the two axes", () => {
    expect(isDragGesture({ x: 5, y: 1 }, 4)).toBe(true);
    expect(isDragGesture({ x: 1, y: 5 }, 4)).toBe(true);
  });

  it("treats negative deltas the same as positive ones", () => {
    expect(isDragGesture({ x: -5, y: 0 }, 4)).toBe(true);
    expect(isDragGesture({ x: -3, y: 0 }, 4)).toBe(false);
  });
});
