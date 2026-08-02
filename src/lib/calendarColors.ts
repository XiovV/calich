export type CalendarColor =
  | "tomato"
  | "flamingo"
  | "banana"
  | "sage"
  | "peacock"
  | "blueberry"
  | "grape"
  | "graphite";

export const CALENDAR_COLORS: CalendarColor[] = [
  "tomato",
  "flamingo",
  "banana",
  "sage",
  "peacock",
  "blueberry",
  "grape",
  "graphite",
];

const CALENDAR_COLOR_CLASSES: Record<CalendarColor, string> = {
  tomato: "bg-calendar-tomato",
  flamingo: "bg-calendar-flamingo",
  banana: "bg-calendar-banana",
  sage: "bg-calendar-sage",
  peacock: "bg-calendar-peacock",
  blueberry: "bg-calendar-blueberry",
  grape: "bg-calendar-grape",
  graphite: "bg-calendar-graphite",
};

export function getCalendarColorClass(color: CalendarColor): string {
  return CALENDAR_COLOR_CLASSES[color];
}

export function getNextUnusedColor(usedColors: CalendarColor[]): CalendarColor {
  const unused = CALENDAR_COLORS.find((color) => !usedColors.includes(color));
  return unused ?? CALENDAR_COLORS[0];
}
