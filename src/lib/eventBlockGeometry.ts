// Events only occupy this much of the day column's width, leaving a strip of
// empty space on the right that's always clickable for creating a new event.
const EVENT_AREA_WIDTH_PERCENT = 85;

export interface EventColumnBox {
  left: number;
  width: number;
}

export function columnLayoutToBox(
  column: number,
  columnCount: number,
): EventColumnBox {
  const width = EVENT_AREA_WIDTH_PERCENT / columnCount;
  return { left: column * width, width };
}
