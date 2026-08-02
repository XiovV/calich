import type { Event } from "./mockEvents";

export interface EventLayout {
  event: Event;
  column: number;
  columnCount: number;
}

export function layoutOverlappingEvents(events: Event[]): EventLayout[] {
  const sorted = [...events].sort(
    (a, b) => a.start.getTime() - b.start.getTime(),
  );

  const layouts: EventLayout[] = [];
  let clusterAssignments: { event: Event; column: number }[] = [];
  let clusterColumnEnds: number[] = [];
  let clusterEnd = -Infinity;

  function flushCluster() {
    const columnCount = clusterColumnEnds.length;
    for (const assignment of clusterAssignments) {
      layouts.push({
        event: assignment.event,
        column: assignment.column,
        columnCount,
      });
    }
    clusterAssignments = [];
    clusterColumnEnds = [];
  }

  for (const event of sorted) {
    if (clusterAssignments.length > 0 && event.start.getTime() >= clusterEnd) {
      flushCluster();
      clusterEnd = -Infinity;
    }

    let columnIndex = clusterColumnEnds.findIndex(
      (endTime) => endTime <= event.start.getTime(),
    );
    if (columnIndex === -1) {
      columnIndex = clusterColumnEnds.length;
      clusterColumnEnds.push(event.end.getTime());
    } else {
      clusterColumnEnds[columnIndex] = event.end.getTime();
    }

    clusterAssignments.push({ event, column: columnIndex });
    clusterEnd = Math.max(clusterEnd, event.end.getTime());
  }
  flushCluster();

  return layouts;
}
