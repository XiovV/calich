import { Popover } from "@base-ui/react/popover";
import { formatDistanceToNow } from "date-fns";
import { Bell } from "lucide-react";
import { useEffect } from "react";
import { useNotificationsStore } from "../../lib/notificationsStore";
import { iconButtonClasses } from "../ui/iconButtonClasses";

// How often the feed is re-polled for newly-fired Notifications while a tab
// is open (ADR-0021). No existing polling precedent elsewhere in the app;
// this is deliberately simple — just a client-side interval, no websocket.
const POLL_INTERVAL_MS = 30_000;

export function NotificationBell() {
  const notifications = useNotificationsStore((state) => state.notifications);
  const fetchNotifications = useNotificationsStore(
    (state) => state.fetchNotifications,
  );
  const markAllSeen = useNotificationsStore((state) => state.markAllSeen);
  const unseenCount = notifications.filter((n) => !n.seen).length;

  useEffect(() => {
    fetchNotifications();
    const interval = setInterval(fetchNotifications, POLL_INTERVAL_MS);
    return () => clearInterval(interval);
  }, [fetchNotifications]);

  return (
    <Popover.Root
      onOpenChange={(open) => {
        if (open && unseenCount > 0) markAllSeen();
      }}
    >
      <Popover.Trigger
        aria-label="Notifications"
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
                  <li
                    key={notification.id}
                    className={`px-3 py-2 ${notification.seen ? "" : "bg-surface-hover"}`}
                  >
                    <p className="text-body text-ink">{notification.title}</p>
                    <p className="text-label-sm text-ink-muted">
                      {notification.occurrenceStart.toLocaleString()} ·{" "}
                      {formatDistanceToNow(notification.firedAt, {
                        addSuffix: true,
                      })}
                    </p>
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
