import { useEffect, useState } from "react";
import { formatDistanceToNow } from "date-fns";
import { Menu } from "@base-ui/react/menu";
import { Check, MoreVertical, Plus, TriangleAlert, Users } from "lucide-react";
import { CalendarPickerModal } from "../../settings/CalendarPickerModal";
import { IconButton } from "../ui/IconButton";
import { iconButtonClasses } from "../ui/iconButtonClasses";
import { canManageCalendar, isLinkedCalendar, shareCountTooltip, type Calendar } from "../../lib/calendar";
import { connectionGroupLabel, groupCalendarsForSidebar, UNRESOLVED_CONNECTION } from "../../lib/calendarGrouping";
import { resolveCalendarFill } from "../../lib/calendarColors";
import { inScopeCalendars } from "../../lib/calendarSetScope";
import { useActiveCalendarSet } from "../../lib/calendarSetsStore";
import { useAuthStore } from "../../lib/authStore";
import { useCalendarsStore } from "../../lib/calendarsStore";
import { useConnectionsStore } from "../../lib/connectionsStore";
import { useEventsStore } from "../../lib/eventsStore";
import { useShellStore } from "../../lib/shellStore";
import { deleteCalendarCascade } from "../../lib/deleteCalendarCascade";
import { leaveCalendarCascade } from "../../lib/leaveCalendarCascade";
import { icsApi, type ExportSummary } from "../../lib/icsApi";
import { ApiError } from "../../lib/apiClient";
import { toast } from "../../lib/toast";
import { ExportSummaryDialog } from "../ExportSummaryDialog";
import { CalendarModal } from "./CalendarModal";
import { CalendarToggle } from "./CalendarToggle";
import { SubscribeCalendarModal } from "./SubscribeCalendarModal";
import { DeleteCalendarConfirmation } from "./DeleteCalendarConfirmation";
import { LeaveCalendarConfirmation } from "./LeaveCalendarConfirmation";
import { ShareCalendarModal } from "./ShareCalendarModal";

// The row's action menu items share this shape so every row type — a click
// away from Edit-only, Edit+Export+Delete, or Edit+Refresh+Unsubscribe — is
// one list instead of three near-duplicate menus (#189).
// UNRESOLVED_CONNECTION's heading (see calendarGrouping.ts) reads "Unknown
// account" here and offers no picker button, since there is no Connection to
// re-open it with.

const menuItemClasses =
  "flex cursor-default items-center px-3 py-1.5 text-body text-ink data-[highlighted]:bg-surface-hover data-[disabled]:pointer-events-none data-[disabled]:opacity-50";
const destructiveMenuItemClasses =
  "flex cursor-default items-center px-3 py-1.5 text-body text-danger data-[highlighted]:bg-danger-50";

// subscriptionErrorReason renders a broken Subscription's sidebar tooltip,
// undefined for a healthy Calendar. The two error classes need different
// sentences (#86): needs_attention means a human must fix something (bad
// credentials, a feed that no longer exists), retrying means the poller
// expects the problem to clear on its own.
function subscriptionErrorReason(calendar: Calendar): string | undefined {
  if (!calendar.errorClass) return undefined;
  return calendar.errorClass === "needs_attention"
    ? `Needs attention: ${calendar.errorMessage}`
    : `Temporarily unreachable, retrying automatically: ${calendar.errorMessage}`;
}

export function CalendarList() {
  const calendars = useCalendarsStore((state) => state.calendars);
  const events = useEventsStore((state) => state.events);
  const accessToken = useAuthStore((state) => state.accessToken);
  const checkedCalendarIds = useShellStore((state) => state.checkedCalendarIds);
  const toggleCalendarChecked = useShellStore(
    (state) => state.toggleCalendarChecked,
  );
  const refreshCalendar = useCalendarsStore((state) => state.refreshCalendar);
  const setCalendarExposure = useCalendarsStore((state) => state.setCalendarExposure);
  // Linked Calendars group by Connection, so the sidebar needs the
  // Connections themselves to label each heading — it can no longer rely on
  // the account Email riding along on every Calendar, which only the list
  // endpoint populates (see linkedCalendarsByConnection below). Settings used
  // to be the only place that fetched these; a User who never opens Settings
  // would otherwise see every heading fall back to "Unknown account".
  const connections = useConnectionsStore((state) => state.connections);
  const fetchConnections = useConnectionsStore((state) => state.fetchConnections);
  useEffect(() => {
    if (!accessToken) return;
    // A failure here costs the heading its label and nothing else, so it is
    // swallowed rather than surfaced: the sidebar's Calendars still render,
    // still group correctly, and still toggle.
    fetchConnections().catch(() => {});
  }, [accessToken, fetchConnections]);
  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [isSubscribeOpen, setIsSubscribeOpen] = useState(false);
  // The Calendar picker, re-opened from a Connection's sidebar heading to
  // add a calendar skipped on the first pass (#295) — the same dialog
  // Settings' Connections Section opens.
  const [pickerConnectionId, setPickerConnectionId] = useState<number | null>(null);
  const [editModalTarget, setEditModalTarget] = useState<Calendar | null>(null);
  // Held as an id rather than a captured Calendar, matching deletingCalendarId
  // and leavingCalendarId: the Share modal's own grants call fetchCalendars(),
  // which replaces every object in the array, so a captured one is stale by
  // construction. editModalTarget can stay an object — CalendarModal copies
  // its props into state on mount.
  const [sharingCalendarId, setSharingCalendarId] = useState<string | null>(null);
  const [deletingCalendarId, setDeletingCalendarId] = useState<string | null>(
    null,
  );
  const [leavingCalendarId, setLeavingCalendarId] = useState<string | null>(
    null,
  );
  const [refreshingCalendarIds, setRefreshingCalendarIds] = useState<
    Set<string>
  >(new Set());
  const [pendingExport, setPendingExport] = useState<{
    calendar: Calendar;
    summary: ExportSummary;
  } | null>(null);
  const [isConfirmingExport, setIsConfirmingExport] = useState(false);

  // Narrowed to the Active Calendar Set before grouping (#303, ADR-0082):
  // an out-of-Set Calendar is absent from every heading below, not merely
  // unchecked — CalendarToggle still reads off the untouched
  // checkedCalendarIds, so a Calendar's toggle value survives switching
  // Sets even while its row is gone. isSetActive gates whether an emptied
  // heading below hides itself: "no Calendars at all" (today's ordinary
  // empty state, headings stay so their Add buttons stay reachable) reads
  // differently from "none left in this Set" (ADR-0082's "hidden rather
  // than merely unchecked").
  const activeCalendarSet = useActiveCalendarSet();
  const isSetActive = activeCalendarSet !== null;
  const visibleCalendars = inScopeCalendars(calendars, activeCalendarSet);

  // Grouped exactly as the Calendar Set membership dialog groups them
  // (#302, ADR-0082) — see calendarGrouping.ts, the one place this logic
  // lives.
  const { myCalendars, subscribedCalendars, linkedByConnection: linkedCalendarsByConnection, sharedCalendars } =
    groupCalendarsForSidebar(visibleCalendars);

  const deletingCalendar = calendars.find(
    (calendar) => calendar.id === deletingCalendarId,
  );
  const leavingCalendar = calendars.find(
    (calendar) => calendar.id === leavingCalendarId,
  );
  const sharingCalendar = calendars.find(
    (calendar) => calendar.id === sharingCalendarId,
  );

  function handleConfirmDelete() {
    if (!deletingCalendar) return;
    deleteCalendarCascade(deletingCalendar.id);
    setDeletingCalendarId(null);
  }

  function handleConfirmLeave() {
    if (!leavingCalendar) return;
    leaveCalendarCascade(leavingCalendar.id);
    setLeavingCalendarId(null);
  }

  async function downloadCalendar(calendar: Calendar) {
    if (!accessToken) return;
    try {
      await icsApi.downloadCalendar(accessToken, calendar.id, calendar.name);
    } catch (error) {
      if (error instanceof ApiError && error.code === "calendar_empty") {
        toast.error(`"${calendar.name}" has no events to download yet.`);
      } else {
        toast.error("Failed to download calendar.");
      }
    }
  }

  // The Export summary pre-flight (#134, ADR-0041): nothing oversized keeps
  // this a single click, exactly like before.
  async function handleDownload(calendar: Calendar) {
    if (!accessToken) return;
    try {
      const summary = await icsApi.calendarOversizedAttachments(accessToken, calendar.id);
      if (summary.count === 0) {
        await downloadCalendar(calendar);
      } else {
        setPendingExport({ calendar, summary });
      }
    } catch {
      toast.error("Failed to check attachments before download.");
    }
  }

  async function handleConfirmExport() {
    if (!pendingExport) return;
    const { calendar } = pendingExport;
    setIsConfirmingExport(true);
    try {
      await downloadCalendar(calendar);
    } finally {
      setIsConfirmingExport(false);
      setPendingExport(null);
    }
  }

  async function handleRefresh(calendar: Calendar) {
    setRefreshingCalendarIds((ids) => new Set(ids).add(calendar.id));
    try {
      await refreshCalendar(calendar.id);
    } catch {
      toast.error(`Failed to refresh "${calendar.name}".`);
    } finally {
      setRefreshingCalendarIds((ids) => {
        const next = new Set(ids);
        next.delete(calendar.id);
        return next;
      });
    }
  }

  function renderCalendarItem(calendar: Calendar) {
    const isSubscribed = Boolean(calendar.sourceUrl);
    const isLinked = isLinkedCalendar(calendar);
    // A Subscribed or Linked Calendar can be refreshed on demand (#85, #288):
    // the manual trigger bypasses the poller's backoff and its
    // unchanged-short-circuit, so it is never a visible no-op.
    const isRefreshable = isSubscribed || isLinked;
    // Rename, recolour, download, refresh, and delete are Calendar
    // management (#111, ADR-0034) — gated on ownership, un-clamped by the
    // Subscription read-only rule, so the Owner of a Subscribed Calendar
    // keeps every one of them.
    const canManage = canManageCalendar(calendar);
    const isRefreshing = refreshingCalendarIds.has(calendar.id);
    const errorReason = subscriptionErrorReason(calendar);
    return (
      <li
        key={calendar.id}
        className="group flex items-center gap-2 rounded-e-full py-2 ps-5 pe-2 transition-colors hover:bg-surface-hover"
      >
        <CalendarToggle
          checked={checkedCalendarIds.has(calendar.id)}
          onCheckedChange={() => toggleCalendarChecked(calendar.id)}
          color={resolveCalendarFill(calendar)}
          aria-label={calendar.name}
        />
        <span className="flex min-w-0 flex-1 flex-col">
          <span className="flex min-w-0 items-center gap-1">
            <span className="min-w-0 truncate text-body text-ink">{calendar.name}</span>
            {errorReason && (
              <span title={errorReason} className="shrink-0">
                <TriangleAlert
                  aria-label={errorReason}
                  className={`size-3 ${
                    calendar.errorClass === "needs_attention" ? "text-danger" : "text-warning"
                  }`}
                />
              </span>
            )}
          </span>
          {!canManage && calendar.ownerName && (
            <span className="truncate text-label-sm text-ink-muted">
              Shared by {calendar.ownerName}
            </span>
          )}
          {isRefreshable && calendar.lastSyncedAt && (
            <span className="truncate text-label-sm text-ink-muted">
              {isRefreshing
                ? "Refreshing…"
                : `Refreshed ${formatDistanceToNow(calendar.lastSyncedAt, { addSuffix: true })}`}
            </span>
          )}
        </span>
        {/* Always visible, unlike the hover-only action buttons below — an
            at-a-glance answer to "who can see my stuff" that you'd have to
            hover to see wouldn't be at a glance (#126). Opens the Share
            modal directly (#221): the badge asks who has Access, and Share
            is where that is both answered and changed. Absent on a shared-in
            row (canManage is false there): that row already says "Shared by
            {owner}" instead. */}
        {canManage && (calendar.shareCount ?? 0) > 0 && (
          <button
            type="button"
            onClick={() => setSharingCalendarId(calendar.id)}
            title={shareCountTooltip(calendar.shareCount ?? 0)}
            aria-label={shareCountTooltip(calendar.shareCount ?? 0)}
            className="flex shrink-0 items-center gap-0.5 rounded-shell-pill px-1 py-0.5 text-label-sm text-ink-muted hover:bg-surface-hover hover:text-ink"
          >
            <Users className="size-3.5" />
            {calendar.shareCount}
          </button>
        )}
        {/* Edit, then Share, then whichever of Refresh/Export the row
            qualifies for (mutually exclusive per ADR-0032), then a divider,
            then the destructive action last — Edit stays first because it's
            the only item every row type has, keeping the menu's first and
            last items in fixed positions no matter which row this is
            (#189). */}
        <Menu.Root>
          <Menu.Trigger
            aria-label={`${calendar.name} actions`}
            title={`${calendar.name} actions`}
            className={iconButtonClasses({
              size: "tiny",
              className:
                "opacity-0 focus-visible:opacity-100 group-hover:opacity-100 data-[popup-open]:opacity-100",
            })}
          >
            <MoreVertical className="size-3.5" />
          </Menu.Trigger>
          <Menu.Portal>
            <Menu.Positioner sideOffset={4} align="end" className="z-[60]">
              <Menu.Popup className="rounded-shell-md border border-border bg-surface py-1 shadow-elevation-2">
                {/* Edit opens the same dialog for everyone (#122): an Owner
                    manages the Calendar itself, while anyone else with
                    Access sees only their own colour, since a personal
                    colour is theirs to set on any Calendar they can see,
                    not just the ones they own. */}
                <Menu.Item
                  onClick={() => setEditModalTarget(calendar)}
                  className={menuItemClasses}
                >
                  Edit
                </Menu.Item>
                {/* Owner-only (ADR-0034), and absent rather than disabled on
                    a shared-in row: sharing isn't a thing that row's viewer
                    could gain by upgrading something, so a greyed item would
                    only advertise a door that is never theirs. An owned
                    *Subscribed* Calendar does get it — the read-only clamp
                    is on its Events, not on who may be given Access to it
                    (#111, ADR-0034). */}
                {canManage && (
                  <Menu.Item
                    onClick={() => setSharingCalendarId(calendar.id)}
                    className={menuItemClasses}
                  >
                    Share
                  </Menu.Item>
                )}
                {isRefreshable && canManage && (
                  <Menu.Item
                    onClick={() => handleRefresh(calendar)}
                    disabled={isRefreshing}
                    className={menuItemClasses}
                  >
                    Refresh
                  </Menu.Item>
                )}
                {/* A Calendar with a Source offers no per-Calendar download
                    (#90, ADR-0032): the feed URL or the Provider is what
                    carries it elsewhere, not a frozen snapshot a Refresh
                    will overwrite anyway. */}
                {!isRefreshable && canManage && (
                  <Menu.Item
                    onClick={() => handleDownload(calendar)}
                    className={menuItemClasses}
                  >
                    Export
                  </Menu.Item>
                )}
                {/* Exposure (#297, ADR-0080): the one surface both the
                    Calendar's Owner and an accessor it was Shared to reach,
                    since an accessor has no Connection of their own under
                    Settings to find this beside. Speaks in terms of
                    devices, never CalDAV. */}
                {isLinked && (
                  <Menu.CheckboxItem
                    checked={Boolean(calendar.exposed)}
                    onCheckedChange={(checked) => setCalendarExposure(calendar.id, checked)}
                    closeOnClick={false}
                    className={menuItemClasses}
                  >
                    <Menu.CheckboxItemIndicator
                      keepMounted
                      className="mr-2 flex size-3.5 shrink-0 items-center justify-center data-[unchecked]:opacity-0"
                    >
                      <Check className="size-3.5" />
                    </Menu.CheckboxItemIndicator>
                    Show on my devices
                  </Menu.CheckboxItem>
                )}
                <div role="separator" className="my-1 border-t border-border" />
                {canManage ? (
                  <Menu.Item
                    onClick={() => setDeletingCalendarId(calendar.id)}
                    className={destructiveMenuItemClasses}
                  >
                    {isSubscribed ? "Unsubscribe" : "Delete"}
                  </Menu.Item>
                ) : (
                  // A shared-in Calendar isn't the viewer's to delete —
                  // Leave renounces their own Share instead, without
                  // touching the Calendar itself or its Owner (#114).
                  <Menu.Item
                    onClick={() => setLeavingCalendarId(calendar.id)}
                    className={destructiveMenuItemClasses}
                  >
                    Leave
                  </Menu.Item>
                )}
              </Menu.Popup>
            </Menu.Positioner>
          </Menu.Portal>
        </Menu.Root>
      </li>
    );
  }

  return (
    <div>
      {/* Hidden only once a Set has narrowed this heading's own group to
          nothing (#303) — with no Set active, it stays put even at zero
          Calendars, exactly as today, so its Add button stays reachable. */}
      {(!isSetActive || myCalendars.length > 0) && (
        <>
          <div className="flex items-center justify-between py-2 ps-5 pe-2">
            <p className="text-label-sm font-medium text-ink-muted">
              My calendars
            </p>
            <IconButton
              size="tiny"
              onClick={() => setIsCreateOpen(true)}
              aria-label="Add calendar"
            >
              <Plus className="size-4" />
            </IconButton>
          </div>
          <ul>{myCalendars.map(renderCalendarItem)}</ul>
        </>
      )}

      {(!isSetActive || subscribedCalendars.length > 0) && (
        <>
          <div className="flex items-center justify-between py-2 ps-5 pe-2">
            <p className="text-label-sm font-medium text-ink-muted">
              Subscribed calendars
            </p>
            <IconButton
              size="tiny"
              onClick={() => setIsSubscribeOpen(true)}
              aria-label="Subscribe to a calendar"
            >
              <Plus className="size-4" />
            </IconButton>
          </div>
          <ul>{subscribedCalendars.map(renderCalendarItem)}</ul>
        </>
      )}

      {/* One heading per Connection, labelled with the connected account's
          Email (#286) — two connected accounts produce two separate
          headings, since each is its own Map entry. */}
      {Array.from(linkedCalendarsByConnection.entries()).map(([connectionId, group]) => {
        const accountEmail = connectionGroupLabel(connectionId, group, connections);
        return (
          <div key={connectionId === UNRESOLVED_CONNECTION ? "unresolved-connection" : connectionId}>
            <div className="flex items-center justify-between py-2 ps-5 pe-2">
              <p className="text-label-sm font-medium text-ink-muted">{accountEmail}</p>
              {connectionId !== UNRESOLVED_CONNECTION && (
                <IconButton
                  size="tiny"
                  onClick={() => setPickerConnectionId(connectionId)}
                  aria-label={`Choose calendars from ${accountEmail}`}
                  title={`Choose calendars from ${accountEmail}`}
                >
                  <Plus className="size-4" />
                </IconButton>
              )}
            </div>
            <ul>{group.map(renderCalendarItem)}</ul>
          </div>
        );
      })}

      {sharedCalendars.length > 0 && (
        <>
          <div className="flex items-center justify-between py-2 ps-5 pe-2">
            <p className="text-label-sm font-medium text-ink-muted">
              Shared with me
            </p>
          </div>
          <ul>{sharedCalendars.map(renderCalendarItem)}</ul>
        </>
      )}

      {isCreateOpen && (
        <CalendarModal mode="create" onClose={() => setIsCreateOpen(false)} />
      )}
      {isSubscribeOpen && (
        <SubscribeCalendarModal onClose={() => setIsSubscribeOpen(false)} />
      )}
      {pickerConnectionId !== null && (
        <CalendarPickerModal
          connectionId={pickerConnectionId}
          onClose={() => setPickerConnectionId(null)}
        />
      )}
      {editModalTarget && (
        <CalendarModal
          mode="edit"
          calendar={editModalTarget}
          onClose={() => setEditModalTarget(null)}
        />
      )}
      {sharingCalendar && (
        <ShareCalendarModal
          calendar={sharingCalendar}
          onClose={() => setSharingCalendarId(null)}
        />
      )}
      {deletingCalendar && (
        <DeleteCalendarConfirmation
          calendar={deletingCalendar}
          eventCount={
            events.filter((event) => event.calendarId === deletingCalendar.id)
              .length
          }
          onConfirm={handleConfirmDelete}
          onClose={() => setDeletingCalendarId(null)}
        />
      )}
      {leavingCalendar && (
        <LeaveCalendarConfirmation
          calendar={leavingCalendar}
          onConfirm={handleConfirmLeave}
          onClose={() => setLeavingCalendarId(null)}
        />
      )}
      {pendingExport && (
        <ExportSummaryDialog
          summary={pendingExport.summary}
          isSubmitting={isConfirmingExport}
          onConfirm={handleConfirmExport}
          onClose={() => setPendingExport(null)}
        />
      )}
    </div>
  );
}
