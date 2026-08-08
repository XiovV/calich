import { useState } from "react";
import { formatDistanceToNow } from "date-fns";
import { Download, LogOut, Pencil, Plus, RefreshCw, Trash2, TriangleAlert, Users } from "lucide-react";
import { Checkbox } from "../ui/Checkbox";
import { IconButton } from "../ui/IconButton";
import { canManageCalendar, shareCountTooltip, type Calendar } from "../../lib/calendar";
import { resolveCalendarFill, toOpaqueHex } from "../../lib/calendarColors";
import { useAuthStore } from "../../lib/authStore";
import { useCalendarsStore } from "../../lib/calendarsStore";
import { useEventsStore } from "../../lib/eventsStore";
import { useShellStore } from "../../lib/shellStore";
import { deleteCalendarCascade } from "../../lib/deleteCalendarCascade";
import { leaveCalendarCascade } from "../../lib/leaveCalendarCascade";
import { icsApi } from "../../lib/icsApi";
import { ApiError } from "../../lib/apiClient";
import { toast } from "../../lib/toast";
import { CalendarModal } from "./CalendarModal";
import { SubscribeCalendarModal } from "./SubscribeCalendarModal";
import { DeleteCalendarConfirmation } from "./DeleteCalendarConfirmation";
import { LeaveCalendarConfirmation } from "./LeaveCalendarConfirmation";

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
  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [isSubscribeOpen, setIsSubscribeOpen] = useState(false);
  // focus: the share-count badge (#126) opens the same edit dialog as the
  // Pencil button, but scrolled to its sharing section — the badge exists
  // to answer "who can see my stuff" at a glance, and clicking through to
  // anywhere else in the dialog wouldn't answer it. Bundled with calendar
  // in one state, rather than a parallel boolean, so the two can't drift
  // out of sync across the three places that set them.
  const [editModalTarget, setEditModalTarget] = useState<{
    calendar: Calendar;
    focus?: "sharing";
  } | null>(null);
  const [deletingCalendarId, setDeletingCalendarId] = useState<string | null>(
    null,
  );
  const [leavingCalendarId, setLeavingCalendarId] = useState<string | null>(
    null,
  );
  const [refreshingCalendarIds, setRefreshingCalendarIds] = useState<
    Set<string>
  >(new Set());

  // A Calendar shared with the viewer is grouped by whose it is, not by
  // where its Events come from (#114) — a Subscribed Calendar someone else
  // owns groups with the shared ones, since its Subscription controls
  // aren't the viewer's in any case.
  const myCalendars = calendars.filter(
    (calendar) => canManageCalendar(calendar) && !calendar.sourceUrl,
  );
  const subscribedCalendars = calendars.filter(
    (calendar) => canManageCalendar(calendar) && calendar.sourceUrl,
  );
  const sharedCalendars = calendars.filter(
    (calendar) => !canManageCalendar(calendar),
  );

  const deletingCalendar = calendars.find(
    (calendar) => calendar.id === deletingCalendarId,
  );
  const leavingCalendar = calendars.find(
    (calendar) => calendar.id === leavingCalendarId,
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

  async function handleDownload(calendar: Calendar) {
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
        {errorReason ? (
          <span title={errorReason} className="shrink-0">
            <TriangleAlert
              aria-label={errorReason}
              className={`size-3 ${
                calendar.errorClass === "needs_attention" ? "text-danger" : "text-warning"
              }`}
            />
          </span>
        ) : (
          <span
            aria-hidden="true"
            style={{ backgroundColor: toOpaqueHex(resolveCalendarFill(calendar)) }}
            className="size-2.5 shrink-0 rounded-shell-pill"
          />
        )}
        <span className="flex min-w-0 flex-1 flex-col">
          <span className="truncate text-body text-ink">{calendar.name}</span>
          {!canManage && calendar.ownerUsername && (
            <span className="truncate text-label-sm text-ink-muted">
              Shared by {calendar.ownerUsername}
            </span>
          )}
          {isSubscribed && calendar.lastSyncedAt && (
            <span className="truncate text-label-sm text-ink-muted">
              Synced{" "}
              {formatDistanceToNow(calendar.lastSyncedAt, { addSuffix: true })}
            </span>
          )}
        </span>
        {/* Always visible, unlike the hover-only action buttons below — an
            at-a-glance answer to "who can see my stuff" that you'd have to
            hover to see wouldn't be at a glance (#126). Absent on a
            shared-in row (canManage is false there): that row already says
            "Shared by {owner}" instead. */}
        {canManage && (calendar.shareCount ?? 0) > 0 && (
          <button
            type="button"
            onClick={() => setEditModalTarget({ calendar, focus: "sharing" })}
            title={shareCountTooltip(calendar.shareCount ?? 0)}
            aria-label={shareCountTooltip(calendar.shareCount ?? 0)}
            className="flex shrink-0 items-center gap-0.5 rounded-shell-pill px-1 py-0.5 text-label-sm text-ink-muted hover:bg-surface-hover hover:text-ink"
          >
            <Users className="size-3.5" />
            {calendar.shareCount}
          </button>
        )}
        {isSubscribed && canManage && (
          <IconButton
            size="tiny"
            onClick={() => handleRefresh(calendar)}
            disabled={isRefreshing}
            aria-label={`Refresh ${calendar.name}`}
            className="opacity-0 focus-visible:opacity-100 group-hover:opacity-100"
          >
            <RefreshCw className={`size-3.5 ${isRefreshing ? "animate-spin" : ""}`} />
          </IconButton>
        )}
        {/* A Subscribed Calendar offers no per-Calendar download (#90,
            ADR-0032): its source URL is what actually carries it elsewhere,
            not a frozen snapshot a Refresh will overwrite anyway. */}
        {!isSubscribed && canManage && (
          <IconButton
            size="tiny"
            onClick={() => handleDownload(calendar)}
            aria-label={`Download ${calendar.name}`}
            className="opacity-0 focus-visible:opacity-100 group-hover:opacity-100"
          >
            <Download className="size-3.5" />
          </IconButton>
        )}
        {/* Edit opens the same dialog for everyone (#122): an Owner manages
            the Calendar itself, while anyone else with Access sees only
            their own colour, since a personal colour is theirs to set on
            any Calendar they can see, not just the ones they own. */}
        <IconButton
          size="tiny"
          onClick={() => setEditModalTarget({ calendar })}
          aria-label={`Edit ${calendar.name}`}
          className="opacity-0 focus-visible:opacity-100 group-hover:opacity-100"
        >
          <Pencil className="size-3.5" />
        </IconButton>
        {canManage && (
          <IconButton
            size="tiny"
            onClick={() => setDeletingCalendarId(calendar.id)}
            aria-label={isSubscribed ? `Unsubscribe from ${calendar.name}` : `Delete ${calendar.name}`}
            className="opacity-0 focus-visible:opacity-100 group-hover:opacity-100"
          >
            <Trash2 className="size-3.5" />
          </IconButton>
        )}
        {/* A shared-in Calendar isn't the viewer's to delete — Leave
            renounces their own Share instead, without touching the
            Calendar itself or its Owner (#114). */}
        {!canManage && (
          <IconButton
            size="tiny"
            onClick={() => setLeavingCalendarId(calendar.id)}
            aria-label={`Leave ${calendar.name}`}
            className="opacity-0 focus-visible:opacity-100 group-hover:opacity-100"
          >
            <LogOut className="size-3.5" />
          </IconButton>
        )}
        <Checkbox
          checked={checkedCalendarIds.has(calendar.id)}
          onCheckedChange={() => toggleCalendarChecked(calendar.id)}
          aria-label={calendar.name}
        />
      </li>
    );
  }

  return (
    <div>
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
      {editModalTarget && (
        <CalendarModal
          mode="edit"
          calendar={editModalTarget.calendar}
          initialFocus={editModalTarget.focus}
          onClose={() => setEditModalTarget(null)}
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
    </div>
  );
}
