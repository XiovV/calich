import type { Occurrence } from "./occurrence";

export interface OccurrenceLayout<T = Occurrence> {
  occurrence: T;
  column: number;
  columnCount: number;
}

/**
 * Column-packs any `start`/`end`-bearing items into overlap clusters. Takes
 * a full `Occurrence[]` for the common case, but also accepts a day's
 * clipped `OccurrenceDaySegment[]` (issue #230) — clustering runs on
 * whatever `start`/`end` the caller hands it, so a midnight-crossing
 * Occurrence's per-day segments overlap-resolve using that day's clipped
 * bounds rather than its true, cross-day span.
 */
export function layoutOverlappingEvents<T extends { start: Date; end: Date }>(
  items: T[],
): OccurrenceLayout<T>[] {
  const sorted = [...items].sort(
    (a, b) => a.start.getTime() - b.start.getTime(),
  );

  const layouts: OccurrenceLayout<T>[] = [];
  let clusterAssignments: { item: T; column: number }[] = [];
  let clusterColumnEnds: number[] = [];
  let clusterEnd = -Infinity;

  function flushCluster() {
    const columnCount = clusterColumnEnds.length;
    for (const assignment of clusterAssignments) {
      layouts.push({
        occurrence: assignment.item,
        column: assignment.column,
        columnCount,
      });
    }
    clusterAssignments = [];
    clusterColumnEnds = [];
  }

  for (const item of sorted) {
    if (
      clusterAssignments.length > 0 &&
      item.start.getTime() >= clusterEnd
    ) {
      flushCluster();
      clusterEnd = -Infinity;
    }

    let columnIndex = clusterColumnEnds.findIndex(
      (endTime) => endTime <= item.start.getTime(),
    );
    if (columnIndex === -1) {
      columnIndex = clusterColumnEnds.length;
      clusterColumnEnds.push(item.end.getTime());
    } else {
      clusterColumnEnds[columnIndex] = item.end.getTime();
    }

    clusterAssignments.push({ item, column: columnIndex });
    clusterEnd = Math.max(clusterEnd, item.end.getTime());
  }
  flushCluster();

  return layouts;
}
