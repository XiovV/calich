import { addDays, setHours, setMinutes, startOfWeek } from "date-fns";

export interface Event {
  id: string;
  calendarId: string;
  title: string;
  start: Date;
  end: Date;
  reminders: number[];
}

function atTime(day: Date, hour: number, minute: number): Date {
  return setMinutes(setHours(day, hour), minute);
}

const weekStart = startOfWeek(new Date());
const monday = addDays(weekStart, 1);
const wednesday = addDays(weekStart, 3);
const thursday = addDays(weekStart, 4);
const friday = addDays(weekStart, 5);

export const mockEvents: Event[] = [
  {
    id: "team-standup",
    calendarId: "work",
    title: "Team standup",
    start: atTime(monday, 9, 0),
    end: atTime(monday, 10, 0),
    reminders: [10],
  },
  {
    id: "manager-1-1",
    calendarId: "work",
    title: "1:1 with manager",
    start: atTime(monday, 9, 30),
    end: atTime(monday, 10, 30),
    reminders: [10],
  },
  {
    id: "lunch-with-sam",
    calendarId: "personal",
    title: "Lunch with Sam",
    start: atTime(wednesday, 12, 0),
    end: atTime(wednesday, 13, 0),
    reminders: [30],
  },
  {
    id: "dentist",
    calendarId: "family",
    title: "Dentist appointment",
    start: atTime(thursday, 15, 0),
    end: atTime(thursday, 16, 30),
    reminders: [60],
  },
  {
    id: "birthday-dinner",
    calendarId: "birthdays",
    title: "Birthday dinner",
    start: atTime(friday, 18, 0),
    end: atTime(friday, 19, 0),
    reminders: [60],
  },
];
