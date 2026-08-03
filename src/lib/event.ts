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
}
