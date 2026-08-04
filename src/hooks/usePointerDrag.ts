import { useEffect, useRef, useState } from "react";
import { CLICK_DISTANCE_THRESHOLD_PX, isDragGesture, type Point } from "../lib/pointerDrag";

export interface DragState {
  delta: Point;
  position: Point;
}

interface UsePointerDragOptions<T> {
  onClick: (payload: T) => void;
  onDrag: (payload: T, state: DragState) => void;
  onMove?: (payload: T, state: DragState) => void;
}

interface DragOrigin<T> {
  payload: T;
  startClientX: number;
  startClientY: number;
}

/**
 * Owns the mousedown-origin -> window mousemove/mouseup lifecycle shared by
 * every draggable Occurrence (issue #42): the caller stashes a payload at
 * `start`, reads the live `delta`/`position` to render its own preview, and
 * gets `onClick`/`onDrag` on release at the single 4px click-vs-drag fork.
 * Gesture-pure — no knowledge of dates or the DOM, so drop-target
 * resolution stays with callers via `state.position` in `onDrag`/`onMove`.
 */
export function usePointerDrag<T>({ onClick, onDrag, onMove }: UsePointerDragOptions<T>) {
  const originRef = useRef<DragOrigin<T> | null>(null);
  const [active, setActive] = useState<T | null>(null);
  const [delta, setDelta] = useState<Point>({ x: 0, y: 0 });
  const [position, setPosition] = useState<Point>({ x: 0, y: 0 });

  function start(payload: T, clientX: number, clientY: number) {
    originRef.current = { payload, startClientX: clientX, startClientY: clientY };
    setDelta({ x: 0, y: 0 });
    setPosition({ x: clientX, y: clientY });
    setActive(payload);
  }

  useEffect(() => {
    if (!originRef.current) return;
    const origin = originRef.current;

    function handleMouseMove(domEvent: MouseEvent) {
      const nextDelta = {
        x: domEvent.clientX - origin.startClientX,
        y: domEvent.clientY - origin.startClientY,
      };
      const nextPosition = { x: domEvent.clientX, y: domEvent.clientY };
      setDelta(nextDelta);
      setPosition(nextPosition);
      onMove?.(origin.payload, { delta: nextDelta, position: nextPosition });
    }

    function handleMouseUp(domEvent: MouseEvent) {
      const state: DragState = {
        delta: {
          x: domEvent.clientX - origin.startClientX,
          y: domEvent.clientY - origin.startClientY,
        },
        position: { x: domEvent.clientX, y: domEvent.clientY },
      };

      if (isDragGesture(state.delta, CLICK_DISTANCE_THRESHOLD_PX)) {
        onDrag(origin.payload, state);
      } else {
        onClick(origin.payload);
      }

      originRef.current = null;
      setActive(null);
    }

    window.addEventListener("mousemove", handleMouseMove);
    window.addEventListener("mouseup", handleMouseUp);
    return () => {
      window.removeEventListener("mousemove", handleMouseMove);
      window.removeEventListener("mouseup", handleMouseUp);
    };
  }, [active, onClick, onDrag, onMove]);

  return { start, active, delta, position };
}
