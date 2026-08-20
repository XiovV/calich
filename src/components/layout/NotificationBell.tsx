import { Popover } from "@base-ui/react/popover";
import { formatDistanceToNow } from "date-fns";
import { Bell, UserPlus } from "lucide-react";
import { useEffect, useState } from "react";
import type { Notification as AppNotification } from "../../lib/notification";
import { useNotificationsStore } from "../../lib/notificationsStore";
import { useShellStore } from "../../lib/shellStore";
import { iconButtonClasses } from "../ui/iconButtonClasses";

// How often the feed is re-polled for newly-fired Notifications while a tab
// is open (ADR-0021). No existing polling precedent elsewhere in the app;
// this is deliberately simple — just a client-side interval, no websocket.
const POLL_INTERVAL_MS = 30_000;

export function NotificationBell() {
  const [open, setOpen] = useState(false);
  const notifications = useNotificationsStore((state) => state.notifications);
  const fetchNotifications = useNotificationsStore(
    (state) => state.fetchNotifications,
  );
  const markAllSeen = useNotificationsStore((state) => state.markAllSeen);
  const requestEventOpen = useShellStore((state) => state.requestEventOpen);
  const unseenCount = notifications.filter((n) => !n.seen).length;

  useEffect(() => {
    fetchNotifications();
    const interval = setInterval(fetchNotifications, POLL_INTERVAL_MS);
    return () => clearInterval(interval);
  }, [fetchNotifications]);

  function handleOpenChange(nextOpen: boolean) {
    setOpen(nextOpen);
    if (nextOpen && unseenCount > 0) markAllSeen();
  }

  function handleNotificationClick(notification: AppNotification) {
    requestEventOpen(notification.eventId);
    setOpen(false);
  }

  return (
    <Popover.Root open={open} onOpenChange={handleOpenChange}>
      <Popover.Trigger
        aria-label="Notifications"
        title="Notifications"
        className={`relative ${iconButtonClasses()}`}
      >
        <Bell className="size-5" />
        {unseenCount > 0 && (
          <span className="absolute right-1.5 top-1.5 size-2 rounded-full bg-danger" />
        )}
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Positioner sideOffset={4} align="end" className="z-[60]">
          <Popover.Popup className="w-80 rounded-shell-md border border-border bg-surface py-1 shadow-elevation-2">
            {notifications.length === 0 ? (
              <p className="px-3 py-2 text-body text-ink-muted">
                No notifications yet.
              </p>
            ) : (
              <ul>
                {notifications.map((notification) => (
                  <li key={notification.id}>
                    <button
                      type="button"
                      onClick={() => handleNotificationClick(notification)}
                      className={`w-full px-3 py-2 text-left ${notification.seen ? "" : "bg-surface-hover"}`}
                    >
                      <span className="flex items-center gap-1.5">
                        {notification.kind === "invite" && (
                          <UserPlus className="size-3.5 shrink-0 text-ink-muted" />
                        )}
                        <span className="text-body text-ink">
                          {notification.title}
                        </span>
                      </span>
                      <p className="text-label-sm text-ink-muted">
                        {notification.kind === "invite"
                          ? "You were invited"
                          : notification.occurrenceStart.toLocaleString()}{" "}
                        ·{" "}
                        {formatDistanceToNow(notification.firedAt, {
                          addSuffix: true,
                        })}
                      </p>
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
  );
}
