// The Event modal's progressive-disclosure rule (#193, ADR-0056): whether
// the modal opens showing only its primary fields (Title, All day,
// Start/End, Calendar) or also its secondary ones (Location, Description,
// Repeat, Reminders, Attachments, Attendees).
//
// "auto-expand if populated" — a pure function recomputed on every open,
// never remembered in shellStore/localStorage/a Preference, so two people
// looking at the same Event see the same modal. Create always passes every
// signal empty/zero, so this trivially returns false for it; the one
// caller-visible entry point is for edit mode, where each signal comes from
// the Occurrence actually being edited except attachmentCount/attendeeCount,
// which — like Attachments and Attendees themselves — belong to the Master
// regardless of which Occurrence is open (ADR-0040, ADR-0046).
export interface EventDisclosureSignals {
  location: string;
  description: string;
  hasRrule: boolean;
  reminderCount: number;
  attachmentCount: number;
  attendeeCount: number;
}

export function hasSecondaryEventFields(signals: EventDisclosureSignals): boolean {
  return (
    signals.location.trim() !== "" ||
    signals.description.trim() !== "" ||
    signals.hasRrule ||
    signals.reminderCount > 0 ||
    signals.attachmentCount > 0 ||
    signals.attendeeCount > 0
  );
}
