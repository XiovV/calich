import { create } from "zustand";
import { useAuthStore } from "./authStore";
import type { Notification as AppNotification } from "./notification";
import { notificationsApi } from "./notificationsApi";
import { makeOptimisticWrite } from "./optimisticWrite";
import { formatDateTime } from "./timeFormat";

interface NotificationsState {
  notifications: AppNotification[];
  // Whether fetchNotifications has completed at least once — the first
  // fetch only establishes the baseline of already-fired Notifications; it
  // must not browser-notify for all of them at once (they didn't just fire
  // while this tab was open).
  initialized: boolean;
  fetchNotifications: () => Promise<void>;
  markAllSeen: () => Promise<void>;
}

function requireAccessToken(): string {
  const accessToken = useAuthStore.getState().accessToken;
  if (!accessToken) throw new Error("Not authenticated.");
  return accessToken;
}

// Mirrors the in-app feed panel (the always-there surface) as a browser
// notification when a tab is open — both read the same Notification records
// (ADR-0021). Permission is requested lazily, on the first Notification that
// actually fires, rather than upfront on load.
function showBrowserNotification(notification: AppNotification) {
  if (typeof Notification === "undefined") return;

  const timeFormat = useAuthStore.getState().user?.timeFormat ?? "24h";
  const body =
    notification.kind === "invite"
      ? "You were invited."
      : `Starts ${formatDateTime(notification.occurrenceStart, timeFormat)}`;

  const fire = () => {
    new Notification(notification.title, { body });
  };

  if (Notification.permission === "granted") {
    fire();
  } else if (Notification.permission !== "denied") {
    Notification.requestPermission().then((permission) => {
      if (permission === "granted") fire();
    });
  }
}

// Binds no Access-change policy (ADR-0067): a Notification has no Calendar
// to name, so a refused write keeps the generic message.
const write = makeOptimisticWrite();

export const useNotificationsStore = create<NotificationsState>((set, get) => ({
  notifications: [],
  initialized: false,

  fetchNotifications: async () => {
    const notifications = await notificationsApi.list(requireAccessToken());
    const wasInitialized = get().initialized;

    // Only a Notification that's both unseen and wasn't already known about
    // locally is "newly fired" — otherwise a poll would re-notify for
    // something the feed panel already showed. The very first fetch has no
    // prior baseline at all, so it never browser-notifies (see `initialized`
    // above) rather than notifying for every already-fired Notification.
    const previousIds = new Set(get().notifications.map((n) => n.id));
    const newlyFired = wasInitialized
      ? notifications.filter((n) => !n.seen && !previousIds.has(n.id))
      : [];

    set({ notifications, initialized: true });
    newlyFired.forEach(showBrowserNotification);
  },

  markAllSeen: async () => {
    const previousNotifications = get().notifications;
    await write({
      apply: () =>
        set({
          notifications: previousNotifications.map((n) => ({ ...n, seen: true })),
        }),
      revert: () => set({ notifications: previousNotifications }),
      dispatch: async () => {
        await notificationsApi.markSeen(requireAccessToken());
      },
      fallbackMessage: "Failed to mark notifications as seen.",
    });
  },
}));
