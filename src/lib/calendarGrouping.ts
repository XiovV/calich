import { canManageCalendar, isLinkedCalendar, type Calendar } from "./calendar";
import type { Connection } from "./connectionsApi";

// UNRESOLVED_CONNECTION is the grouping key for a Linked Calendar whose
// Connection id is missing from the payload — a state the server should
// never produce, kept so that such a Calendar still renders instead of
// disappearing. See connectionGroupLabel.
export const UNRESOLVED_CONNECTION = Symbol("unresolved connection");

export interface GroupedCalendars {
  myCalendars: Calendar[];
  subscribedCalendars: Calendar[];
  linkedByConnection: Map<number | typeof UNRESOLVED_CONNECTION, Calendar[]>;
  sharedCalendars: Calendar[];
}

// groupCalendarsForSidebar buckets calendars the way the sidebar does
// (#114, #286): My calendars, Subscribed, one Linked group per Connection,
// and Shared with me. The one place this grouping lives, so the sidebar
// (CalendarList) and the Calendar Set membership dialog (#302, ADR-0082)
// can never drift apart — ADR-0082's "existing origin grouping" reuse.
//
// A Calendar shared with the viewer groups by whose it is, not by where its
// Events come from (#114) — a Subscribed or Linked Calendar someone else
// owns groups with the shared ones, since its Subscription/Connection
// controls aren't the viewer's in any case.
export function groupCalendarsForSidebar(calendars: Calendar[]): GroupedCalendars {
  const myCalendars = calendars.filter(
    (calendar) => canManageCalendar(calendar) && !calendar.sourceUrl && !isLinkedCalendar(calendar),
  );
  const subscribedCalendars = calendars.filter(
    (calendar) => canManageCalendar(calendar) && Boolean(calendar.sourceUrl),
  );

  // Keyed on the Connection's id, never on the account Email — see
  // CalendarList's own history with this (#295, #286): the Email is a
  // denormalized display field only the list endpoint populates.
  const linkedByConnection = new Map<number | typeof UNRESOLVED_CONNECTION, Calendar[]>();
  for (const calendar of calendars) {
    if (!canManageCalendar(calendar) || !isLinkedCalendar(calendar)) continue;
    const key = calendar.connectionId ?? UNRESOLVED_CONNECTION;
    const group = linkedByConnection.get(key) ?? [];
    group.push(calendar);
    linkedByConnection.set(key, group);
  }

  const sharedCalendars = calendars.filter((calendar) => !canManageCalendar(calendar));

  return { myCalendars, subscribedCalendars, linkedByConnection, sharedCalendars };
}

// connectionGroupLabel resolves a linkedByConnection entry's heading label,
// at render rather than carried on the rows. The Connections store is the
// durable source; a Calendar's own connectionAccountEmail stands in while
// that first fetch is still in flight, and "Unknown account" is a genuine
// unresolved-Connection state rather than an artefact of which endpoint
// answered last.
export function connectionGroupLabel(
  connectionId: number | typeof UNRESOLVED_CONNECTION,
  group: Calendar[],
  connections: Connection[],
): string {
  return (
    (connectionId !== UNRESOLVED_CONNECTION
      ? connections.find((connection) => connection.id === connectionId)?.accountEmail
      : undefined) ??
    group.find((calendar) => calendar.connectionAccountEmail)?.connectionAccountEmail ??
    "Unknown account"
  );
}
