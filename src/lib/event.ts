export interface Event {
  id: string;
  calendarId: string;
  title: string;
  start: Date;
  end: Date;
  // The iCalendar RRULE (RFC 5545) describing how this Event repeats, or
  // undefined when it does not recur. The Event's start/end define the first
  // Occurrence and the duration every Occurrence inherits. See ADR-0016.
  rrule?: string;
  // parentId/recurrenceId are set together, only on an Override: a standalone
  // Event that replaces one Occurrence of its parent's series, keyed to that
  // Occurrence's start (its RECURRENCE-ID). Undefined on a Master. See
  // ADR-0016 and CONTEXT.md.
  parentId?: string;
  recurrenceId?: Date;
  // Exdates lists a Master's cancelled Occurrence starts (Exceptions).
  // Undefined/empty on an Override.
  exdates?: Date[];
}
