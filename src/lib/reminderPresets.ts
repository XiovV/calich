export interface ReminderPreset {
  minutes: number;
  label: string;
}

export const REMINDER_PRESETS: ReminderPreset[] = [
  { minutes: 0, label: "At time of event" },
  { minutes: 5, label: "5 minutes before" },
  { minutes: 10, label: "10 minutes before" },
  { minutes: 30, label: "30 minutes before" },
  { minutes: 60, label: "1 hour before" },
];
