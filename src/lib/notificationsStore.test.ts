import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("./notificationsApi", () => ({
  notificationsApi: {
    list: vi.fn(),
    markSeen: vi.fn(),
  },
}));

vi.mock("./toast", () => ({
  toast: { error: vi.fn() },
}));

const { notificationsApi } = await import("./notificationsApi");
const { toast } = await import("./toast");
const { useAuthStore } = await import("./authStore");
const { useNotificationsStore } = await import("./notificationsStore");

const standup = {
  id: 1,
  eventId: "evt-1",
  kind: "reminder" as const,
  title: "Standup",
  occurrenceStart: new Date("2026-01-01T09:00:00Z"),
  firedAt: new Date("2026-01-01T08:50:00Z"),
  seen: false,
};

const inviteNotification = {
  id: 2,
  eventId: "evt-2",
  kind: "invite" as const,
  title: "Planning",
  occurrenceStart: null,
  firedAt: new Date("2026-01-01T08:50:00Z"),
  seen: false,
};

function resetStore() {
  useNotificationsStore.setState({ notifications: [], initialized: false });
  useAuthStore.setState({ accessToken: "token-123" });
}

beforeEach(() => {
  vi.clearAllMocks();
  resetStore();
});

describe("fetchNotifications", () => {
  it("loads notifications from the API", async () => {
    vi.mocked(notificationsApi.list).mockResolvedValue([standup]);

    await useNotificationsStore.getState().fetchNotifications();

    expect(useNotificationsStore.getState().notifications).toEqual([standup]);
    expect(notificationsApi.list).toHaveBeenCalledWith("token-123");
  });

  function stubNotification() {
    const notificationSpy = vi.fn();
    vi.stubGlobal(
      "Notification",
      Object.assign(notificationSpy, {
        permission: "granted",
        requestPermission: vi.fn(),
      }),
    );
    return notificationSpy;
  }

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("does not browser-notify on the very first fetch, even for unseen Notifications", async () => {
    // The first fetch establishes the baseline of already-fired Notifications
    // — none of them just fired while this tab was open.
    vi.mocked(notificationsApi.list).mockResolvedValue([standup]);
    const notificationSpy = stubNotification();

    await useNotificationsStore.getState().fetchNotifications();

    expect(notificationSpy).not.toHaveBeenCalled();
  });

  it("does not re-notify on a later fetch for a Notification already known locally", async () => {
    useNotificationsStore.setState({ notifications: [standup], initialized: true });
    vi.mocked(notificationsApi.list).mockResolvedValue([standup]);
    const notificationSpy = stubNotification();

    await useNotificationsStore.getState().fetchNotifications();

    expect(notificationSpy).not.toHaveBeenCalled();
  });

  it("fires a browser notification for an unseen Notification that appears on a fetch after the first", async () => {
    useNotificationsStore.setState({ notifications: [], initialized: true });
    vi.mocked(notificationsApi.list).mockResolvedValue([standup]);
    const notificationSpy = stubNotification();

    await useNotificationsStore.getState().fetchNotifications();

    expect(notificationSpy).toHaveBeenCalledWith(
      "Standup",
      expect.objectContaining({ body: expect.any(String) }),
    );
  });

  it("does not fire a browser notification for an already-seen Notification", async () => {
    useNotificationsStore.setState({ notifications: [], initialized: true });
    vi.mocked(notificationsApi.list).mockResolvedValue([{ ...standup, seen: true }]);
    const notificationSpy = stubNotification();

    await useNotificationsStore.getState().fetchNotifications();

    expect(notificationSpy).not.toHaveBeenCalled();
  });

  it("fires a browser notification for an invite Notification without an occurrenceStart", async () => {
    useNotificationsStore.setState({ notifications: [], initialized: true });
    vi.mocked(notificationsApi.list).mockResolvedValue([inviteNotification]);
    const notificationSpy = stubNotification();

    await useNotificationsStore.getState().fetchNotifications();

    expect(notificationSpy).toHaveBeenCalledWith(
      "Planning",
      expect.objectContaining({ body: expect.any(String) }),
    );
  });
});

describe("markAllSeen", () => {
  it("marks all notifications seen immediately, before the API call resolves", () => {
    useNotificationsStore.setState({ notifications: [standup] });
    let resolveMarkSeen: () => void = () => {};
    vi.mocked(notificationsApi.markSeen).mockReturnValue(
      new Promise((resolve) => {
        resolveMarkSeen = () => resolve();
      }),
    );

    const promise = useNotificationsStore.getState().markAllSeen();

    expect(useNotificationsStore.getState().notifications).toEqual([
      { ...standup, seen: true },
    ]);

    resolveMarkSeen();
    return promise;
  });

  it("reverts and shows a toast if the API call fails", async () => {
    useNotificationsStore.setState({ notifications: [standup] });
    vi.mocked(notificationsApi.markSeen).mockRejectedValue(new Error("network error"));

    await useNotificationsStore.getState().markAllSeen();

    expect(useNotificationsStore.getState().notifications).toEqual([standup]);
    expect(toast.error).toHaveBeenCalledWith("Failed to mark notifications as seen.");
  });
});
