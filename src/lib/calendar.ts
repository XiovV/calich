export interface Calendar {
  id: string;
  name: string;
  color: string;
  // access is the caller's resolved Access to this Calendar (ADR-0034):
  // "owner", "editor", or "viewer". Gates Event writes — see
  // canWriteCalendarEvents. Deliberately lossy about ownership: a
  // Subscribed Calendar clamps its Owner's access to "viewer" too (#111).
  access?: "owner" | "editor" | "viewer";
  // isOwner is un-clamped ownership (#111) — true for the Owner even of a
  // Subscribed Calendar, whose access above reads "viewer". The only
  // correct basis for gating Calendar management — see canManageCalendar.
  isOwner?: boolean;
  // ownerName is the Calendar's Owner's display Name (#111).
  ownerName?: string;
  // shareCount is how many Shares the Calendar carries — whether more than
  // one person would be notified by a change to it (#111).
  shareCount?: number;
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

// canWriteCalendarEvents reports whether the caller may create, edit, and
// delete calendar's Events (#111, ADR-0034) — derived from access, never
// from isSubscribedCalendar. A Subscribed Calendar's read-only-ness folds
// in for free: the server already clamps its Access to "viewer" for Owner
// and Editor alike (ADR-0032). Accepts undefined the same way
// isSubscribedCalendar does, and treats a Calendar with no resolved access
// as unwritable rather than assuming permissive defaults.
export function canWriteCalendarEvents(calendar: Calendar | undefined): boolean {
  return calendar?.access === "editor" || calendar?.access === "owner";
}

// canManageCalendar reports whether the caller may manage calendar itself —
// rename, recolour, download, delete, Subscription editing, refresh, and
// sharing (#111, ADR-0034). Derived from isOwner, never from access: a
// Subscribed Calendar's Owner keeps every one of these even though their
// access reads "viewer" — the trap this predicate exists to avoid.
export function canManageCalendar(calendar: Calendar | undefined): boolean {
  return Boolean(calendar?.isOwner);
}

// calendarPickerLabel is a Calendar picker option's label (#127): qualified
// with its Owner's Name when the caller doesn't own it, so two
// same-named Calendars in one picker (yours and one shared with you) don't
// read as indistinguishable. Uses canManageCalendar, not access, for the
// same reason canManageCalendar itself does — a Subscribed Calendar's Owner
// must see no qualifier on their own Calendar even though their access
// reads "viewer". Always qualifies a Calendar the caller doesn't own,
// whether or not its name collides with anything else visible: a label that
// only appears on collision would silently change if the other Calendar
// were renamed or removed. ownerName is optional on Calendar (like every
// other reader of it — CalendarList, CalendarModal,
// LeaveCalendarConfirmation), so an unresolved one falls back to the bare
// name rather than rendering "(undefined)".
export function calendarPickerLabel(calendar: Calendar): string {
  if (canManageCalendar(calendar) || !calendar.ownerName) return calendar.name;
  return `${calendar.name} (${calendar.ownerName})`;
}

// CalendarReadOnlyReason explains why an unwritable Calendar's Events can't
// be written — for copy only (e.g. the Event dialog's explanation), never
// for gating (#111). "subscription" wins over "viewer" when a Calendar is
// both, since a subscriber already knows their feed is read-only in a way
// a Viewer without an Owner's context does not (#109).
export type CalendarReadOnlyReason = "subscription" | "viewer" | undefined;

export function calendarReadOnlyReason(
  calendar: Calendar | undefined,
): CalendarReadOnlyReason {
  if (canWriteCalendarEvents(calendar)) return undefined;
  if (isSubscribedCalendar(calendar)) return "subscription";
  return "viewer";
}

// calendarHasOtherRecipients reports whether more than one person has
// Access to calendar — the Attachment uploader (#132, ADR-0040) renders only
// then, so a single-user Calendar never sees an uploader with nobody else to
// share the file with. True either because the caller isn't this Calendar's
// Owner (so someone else already has Access) or because the Owner has
// granted at least one Share. Uses canManageCalendar,
// not access, for the same reason canManageCalendar itself does: a
// Subscribed Calendar's Owner reads "viewer" in access but is still its only
// Owner. Accepts undefined the same way canWriteCalendarEvents does, and
// treats an unresolved Calendar as having no other recipients rather than
// assuming a permissive default.
export function calendarHasOtherRecipients(calendar: Calendar | undefined): boolean {
  if (!calendar) return false;
  return !canManageCalendar(calendar) || (calendar.shareCount ?? 0) > 0;
}

// shareCountTooltip is the sidebar share badge's tooltip text (#126):
// singular for exactly one Share, since "Shared with 1 people" reads wrong.
export function shareCountTooltip(shareCount: number): string {
  return `Shared with ${shareCount} ${shareCount === 1 ? "person" : "people"}`;
}

// CalendarPickerEmptyReason explains why the Event modal's Calendar picker
// has no options to offer (#174) — the remedy differs by cause, so the
// empty state must distinguish them rather than showing one generic
// message: "none" means create one; "hidden" means show one already owned,
// which must never suggest creating a Calendar the caller already has;
// "unwritable" means every checked Calendar is Subscribed or Viewer-only.
// "hidden" wins over "unwritable" — a Calendar that's both unchecked and
// unwritable is still reachable by checking it. Meaningful only when the
// picker's own options (checked Calendars filtered through
// canWriteCalendarEvents, same as here) are already empty — the caller
// establishes that before asking why; this just distinguishes the causes.
export type CalendarPickerEmptyReason = "none" | "hidden" | "unwritable";

export function calendarPickerEmptyReason(
  calendars: Calendar[],
  checkedCalendarIds: Set<string>,
): CalendarPickerEmptyReason {
  if (calendars.length === 0) return "none";
  if (getCheckedCalendars(calendars, checkedCalendarIds).length === 0) return "hidden";
  return "unwritable";
}

// defaultCalendarId picks the Calendar a new Event should default to, from
// the caller's writable, checked Calendars — preferring one they own, since
// an Editor Share on someone else's Calendar may otherwise sort first and
// default a new Event onto a Calendar that isn't theirs (#115).
export function defaultCalendarId(calendars: Calendar[]): string {
  const owned = calendars.find((calendar) => canManageCalendar(calendar));
  return (owned ?? calendars[0])?.id ?? "";
}
