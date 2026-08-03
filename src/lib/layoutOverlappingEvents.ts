import type { Occurrence } from "./occurrence";

export interface OccurrenceLayout {
  occurrence: Occurrence;
  column: number;
  columnCount: number;
}

export function layoutOverlappingEvents(
  occurrences: Occurrence[],
): OccurrenceLayout[] {
  const sorted = [...occurrences].sort(
    (a, b) => a.start.getTime() - b.start.getTime(),
  );

  const layouts: OccurrenceLayout[] = [];
  let clusterAssignments: { occurrence: Occurrence; column: number }[] = [];
  let clusterColumnEnds: number[] = [];
  let clusterEnd = -Infinity;

  function flushCluster() {
    const columnCount = clusterColumnEnds.length;
    for (const assignment of clusterAssignments) {
      layouts.push({
        occurrence: assignment.occurrence,
        column: assignment.column,
        columnCount,
      });
    }
    clusterAssignments = [];
    clusterColumnEnds = [];
  }

  for (const occurrence of sorted) {
    if (
      clusterAssignments.length > 0 &&
      occurrence.start.getTime() >= clusterEnd
    ) {
      flushCluster();
      clusterEnd = -Infinity;
    }

    let columnIndex = clusterColumnEnds.findIndex(
      (endTime) => endTime <= occurrence.start.getTime(),
    );
    if (columnIndex === -1) {
      columnIndex = clusterColumnEnds.length;
      clusterColumnEnds.push(occurrence.end.getTime());
    } else {
      clusterColumnEnds[columnIndex] = occurrence.end.getTime();
    }

    clusterAssignments.push({ occurrence, column: columnIndex });
    clusterEnd = Math.max(clusterEnd, occurrence.end.getTime());
  }
  flushCluster();

  return layouts;
}
