export interface Calendar {
  id: string;
  name: string;
  color: string;
  // sourceUrl is set only on a Subscribed Calendar (#83) — an ordinary
  // Calendar has none. Always masked when it carries a password.
  sourceUrl?: string;
  // lastSyncedAt is when a Refresh last completed successfully against
  // sourceUrl, whether or not it changed anything (#85). Undefined until the
  // first Refresh runs, and always undefined on an ordinary Calendar.
  lastSyncedAt?: string;
  // errorClass is "needs_attention" or "retrying" when the background
  // poller's (or a manual) Refresh last failed, undefined while healthy or
  // for an ordinary Calendar (#86). errorMessage is the reason.
  errorClass?: "needs_attention" | "retrying";
  errorMessage?: string;
  // keepAlarms is a Subscribed Calendar's opt-in to honouring the feed's
  // VALARMs on both Channels (including Email), off by default and
  // meaningless on an ordinary Calendar (#87).
  keepAlarms?: boolean;
}

// isBrokenSubscription reports whether calendar's last Refresh failed —
// the sidebar shows a warning triangle instead of the ordinary colour dot
// in this state (#86, ADR-0033).
export function isBrokenSubscription(calendar: Calendar | undefined): boolean {
  return Boolean(calendar?.errorClass);
}

export function getCalendarById(
  calendars: Calendar[],
  id: string,
): Calendar | undefined {
  return calendars.find((calendar) => calendar.id === id);
}

export function getCheckedCalendars(
  calendars: Calendar[],
  checkedCalendarIds: Set<string>,
): Calendar[] {
  return calendars.filter((calendar) => checkedCalendarIds.has(calendar.id));
}

// isSubscribedCalendar reports whether calendar is a Subscribed Calendar
// (#83, ADR-0032) — read-only, since only Refresh writes its Events (#84).
// Accepts undefined (an unresolved calendarId) so callers resolving a
// Calendar by id don't need their own separate undefined check before
// asking this.
export function isSubscribedCalendar(calendar: Calendar | undefined): boolean {
  return Boolean(calendar?.sourceUrl);
}
