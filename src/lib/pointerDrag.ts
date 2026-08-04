export interface Point {
  x: number;
  y: number;
}

export const CLICK_DISTANCE_THRESHOLD_PX = 4;

/**
 * True once a pointer has moved far enough from its mousedown origin to
 * count as a drag rather than a click, per axis's larger displacement
 * (issue #42). Shared by every `usePointerDrag` caller so the threshold
 * lives in exactly one place.
 */
export function isDragGesture(delta: Point, thresholdPx: number): boolean {
  return Math.max(Math.abs(delta.x), Math.abs(delta.y)) >= thresholdPx;
}
