import type { Reminder } from "../lib/event";
import { ReminderRow } from "./ReminderRow";

/** A Reminder plus a local id, so its row keeps stable identity across
 * add/remove/reorder in a Reminders section (Reminder itself has no id —
 * it's just an offset + channel pair, ADR-0020). Shared by the Event
 * modal's own Reminders section and the Calendar modal's two Default
 * reminders lists (ADR-0064). */
export type ReminderDraft = Reminder & { draftId: string };

export function toReminderDrafts(reminders: Reminder[]): ReminderDraft[] {
  return reminders.map((reminder) => ({ ...reminder, draftId: crypto.randomUUID() }));
}

export function fromReminderDrafts(drafts: ReminderDraft[]): Reminder[] {
  return drafts.map(({ offsetMinutes, channel }) => ({ offsetMinutes, channel }));
}

interface ReminderListEditorProps {
  reminders: ReminderDraft[];
  onChange: (reminders: ReminderDraft[]) => void;
  // Whether the Email Channel can be offered — the user has an account
  // email set *and* the self-hoster has SMTP configured (ADR-0021).
  emailAvailable: boolean;
  // Seed offset for a freshly added row — 10 minutes for a timed list, a
  // day for an all-day one (ADR-0020's anchor differs, ADR-0064).
  defaultOffsetMinutes: number;
}

// ReminderListEditor is one Reminders list's add/change/remove control —
// the Event modal's own Reminders section (ADR-0064, #211) and each of the
// Calendar modal's two Default reminders lists, timed and all-day
// (ADR-0064), share this one control rather than three copies of the same
// add/change/remove wiring.
export function ReminderListEditor({
  reminders,
  onChange,
  emailAvailable,
  defaultOffsetMinutes,
}: ReminderListEditorProps) {
  function handleAdd() {
    onChange([
      ...reminders,
      { draftId: crypto.randomUUID(), offsetMinutes: defaultOffsetMinutes, channel: "notification" },
    ]);
  }

  function handleChange(draftId: string, reminder: Reminder) {
    onChange(reminders.map((r) => (r.draftId === draftId ? { ...reminder, draftId } : r)));
  }

  function handleRemove(draftId: string) {
    onChange(reminders.filter((r) => r.draftId !== draftId));
  }

  return (
    <>
      {reminders.length > 0 && (
        <div className="flex flex-col gap-2">
          {reminders.map((reminder) => (
            <ReminderRow
              key={reminder.draftId}
              reminder={reminder}
              emailAvailable={emailAvailable}
              onChange={(next) => handleChange(reminder.draftId, next)}
              onRemove={() => handleRemove(reminder.draftId)}
            />
          ))}
        </div>
      )}
      <button
        type="button"
        onClick={handleAdd}
        className="mt-1.5 cursor-pointer text-label-sm text-accent hover:underline"
      >
        Add reminder
      </button>
    </>
  );
}
