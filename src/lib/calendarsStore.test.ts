import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("./calendarsApi", () => ({
  calendarsApi: {
    list: vi.fn(),
    create: vi.fn(),
    update: vi.fn(),
    updateColor: vi.fn(),
    remove: vi.fn(),
    leave: vi.fn(),
    subscribe: vi.fn(),
    get: vi.fn(),
    refresh: vi.fn(),
    share: vi.fn(),
    revokeShare: vi.fn(),
  },
}));

vi.mock("./eventsApi", () => ({
  eventsApi: {
    list: vi.fn(),
  },
}));

vi.mock("./toast", () => ({
  toast: { error: vi.fn() },
}));

const { calendarsApi } = await import("./calendarsApi");
const { eventsApi } = await import("./eventsApi");
const { toast } = await import("./toast");
const { useAuthStore } = await import("./authStore");
const { useCalendarsStore } = await import("./calendarsStore");
const { useEventsStore } = await import("./eventsStore");
const { ApiError } = await import("./apiClient");

const personal = { id: "cal-1", name: "Personal", color: "peacock" as const };
const work = { id: "cal-2", name: "Work", color: "tomato" as const };

function resetStore() {
  useCalendarsStore.setState({ calendars: [] });
  useEventsStore.setState({ events: [] });
  useAuthStore.setState({ accessToken: "token-123" });
}

beforeEach(() => {
  vi.clearAllMocks();
  resetStore();
});

describe("fetchCalendars", () => {
  it("loads calendars from the API", async () => {
    vi.mocked(calendarsApi.list).mockResolvedValue([personal, work]);

    await useCalendarsStore.getState().fetchCalendars();

    expect(useCalendarsStore.getState().calendars).toEqual([personal, work]);
    expect(calendarsApi.list).toHaveBeenCalledWith("token-123");
  });
});

describe("addCalendar", () => {
  it("adds the calendar immediately, before the API call resolves", () => {
    let resolveCreate: () => void = () => {};
    vi.mocked(calendarsApi.create).mockReturnValue(
      new Promise((resolve) => {
        resolveCreate = () => resolve(personal);
      }),
    );

    const promise = useCalendarsStore.getState().addCalendar(personal);

    expect(useCalendarsStore.getState().calendars).toEqual([personal]);

    resolveCreate();
    return promise;
  });

  it("rolls back and shows a toast when the API call fails", async () => {
    vi.mocked(calendarsApi.create).mockRejectedValue(new Error("network error"));

    await useCalendarsStore.getState().addCalendar(personal);

    expect(useCalendarsStore.getState().calendars).toEqual([]);
    expect(toast.error).toHaveBeenCalledWith('Failed to create calendar "Personal".');
  });

  it("resolves true on success and false on failure", async () => {
    vi.mocked(calendarsApi.create).mockResolvedValueOnce(personal);
    await expect(useCalendarsStore.getState().addCalendar(personal)).resolves.toBe(true);

    vi.mocked(calendarsApi.create).mockRejectedValueOnce(new Error("network error"));
    await expect(useCalendarsStore.getState().addCalendar(personal)).resolves.toBe(false);
  });
});

describe("updateCalendar", () => {
  it("updates the calendar immediately", () => {
    useCalendarsStore.setState({ calendars: [personal] });
    let resolveUpdate: () => void = () => {};
    vi.mocked(calendarsApi.update).mockReturnValue(
      new Promise((resolve) => {
        resolveUpdate = () => resolve({ ...personal, name: "Renamed" });
      }),
    );

    const promise = useCalendarsStore
      .getState()
      .updateCalendar("cal-1", { name: "Renamed", color: "peacock" });

    expect(useCalendarsStore.getState().calendars[0].name).toBe("Renamed");

    resolveUpdate();
    return promise;
  });

  it("rolls back and shows a toast when the API call fails", async () => {
    useCalendarsStore.setState({ calendars: [personal] });
    vi.mocked(calendarsApi.update).mockRejectedValue(new Error("network error"));

    await useCalendarsStore
      .getState()
      .updateCalendar("cal-1", { name: "Renamed", color: "peacock" });

    expect(useCalendarsStore.getState().calendars).toEqual([personal]);
    expect(toast.error).toHaveBeenCalledWith("Failed to update calendar.");
  });

  // #88, ADR-0032: the User typed a raw, possibly password-bearing URL —
  // it must never show up in local state, only the masked value the
  // server's response carries.
  it("applies the server's masked sourceUrl rather than the raw typed url", async () => {
    const subscribed = {
      id: "cal-3",
      name: "Team Holidays",
      color: "#8E44ADFF",
      sourceUrl: "https://old.example.com/••••••@feed.ics",
    };
    useCalendarsStore.setState({ calendars: [subscribed] });
    vi.mocked(calendarsApi.update).mockResolvedValue({
      ...subscribed,
      sourceUrl: "https://new.example.com/••••••@feed.ics",
    });

    await useCalendarsStore.getState().updateCalendar("cal-3", {
      name: "Team Holidays",
      color: "#8E44ADFF",
      url: "https://new.example.com/alice:s3cret@feed.ics",
    });

    const stored = useCalendarsStore.getState().calendars[0];
    expect(stored.sourceUrl).toBe("https://new.example.com/••••••@feed.ics");
    expect(stored.sourceUrl).not.toContain("s3cret");
  });
});

describe("setCalendarColor", () => {
  it("applies the color immediately", () => {
    useCalendarsStore.setState({ calendars: [personal] });
    let resolveUpdate: () => void = () => {};
    vi.mocked(calendarsApi.updateColor).mockReturnValue(
      new Promise((resolve) => {
        resolveUpdate = () => resolve({ ...personal, color: "#654321FF" });
      }),
    );

    const promise = useCalendarsStore.getState().setCalendarColor("cal-1", "#654321");

    expect(useCalendarsStore.getState().calendars[0].color).toBe("#654321");
    expect(calendarsApi.updateColor).toHaveBeenCalledWith("token-123", "cal-1", "#654321");

    resolveUpdate();
    return promise;
  });

  it("rolls back and shows a toast when the API call fails", async () => {
    useCalendarsStore.setState({ calendars: [personal] });
    vi.mocked(calendarsApi.updateColor).mockRejectedValue(new Error("network error"));

    await useCalendarsStore.getState().setCalendarColor("cal-1", "#654321");

    expect(useCalendarsStore.getState().calendars).toEqual([personal]);
    expect(toast.error).toHaveBeenCalledWith("Failed to update calendar color.");
  });
});

describe("removeCalendar", () => {
  it("removes the calendar immediately", () => {
    useCalendarsStore.setState({ calendars: [personal] });
    let resolveRemove: () => void = () => {};
    vi.mocked(calendarsApi.remove).mockReturnValue(
      new Promise((resolve) => {
        resolveRemove = () => resolve(undefined);
      }),
    );

    const promise = useCalendarsStore.getState().removeCalendar("cal-1");

    expect(useCalendarsStore.getState().calendars).toEqual([]);

    resolveRemove();
    return promise;
  });

  it("rolls back and shows a toast when the API call fails", async () => {
    useCalendarsStore.setState({ calendars: [personal] });
    vi.mocked(calendarsApi.remove).mockRejectedValue(new Error("network error"));

    await useCalendarsStore.getState().removeCalendar("cal-1");

    expect(useCalendarsStore.getState().calendars).toEqual([personal]);
    expect(toast.error).toHaveBeenCalledWith("Failed to delete calendar.");
  });

  // ADR-0067 brought #116's Access-change handling to Calendar writes, which
  // previously said only "Failed to delete calendar." and refetched nothing,
  // leaving a sidebar that still showed a Calendar the caller had lost.
  it("names the calendar and refetches when access changed underneath", async () => {
    useCalendarsStore.setState({ calendars: [personal] });
    vi.mocked(calendarsApi.remove).mockRejectedValue(
      new ApiError(403, "forbidden", "Forbidden"),
    );
    vi.mocked(calendarsApi.list).mockResolvedValue([]);

    const succeeded = await useCalendarsStore.getState().removeCalendar("cal-1");

    expect(succeeded).toBe(false);
    // The name resolves from the rolled-back state, before the refetch that
    // may well drop the Calendar entirely.
    expect(toast.error).toHaveBeenCalledWith(
      'Your access to "Personal" has changed. Refreshing calendars.',
    );
    expect(calendarsApi.list).toHaveBeenCalled();
  });

  it("keeps the generic message when the failure is not an access change", async () => {
    useCalendarsStore.setState({ calendars: [personal] });
    vi.mocked(calendarsApi.remove).mockRejectedValue(
      new ApiError(500, "internal", "Server error"),
    );

    await useCalendarsStore.getState().removeCalendar("cal-1");

    expect(toast.error).toHaveBeenCalledWith("Failed to delete calendar.");
    expect(calendarsApi.list).not.toHaveBeenCalled();
  });
});

describe("leaveCalendar", () => {
  it("removes the calendar immediately", () => {
    useCalendarsStore.setState({ calendars: [personal] });
    let resolveLeave: () => void = () => {};
    vi.mocked(calendarsApi.leave).mockReturnValue(
      new Promise((resolve) => {
        resolveLeave = () => resolve(undefined);
      }),
    );

    const promise = useCalendarsStore.getState().leaveCalendar("cal-1");

    expect(useCalendarsStore.getState().calendars).toEqual([]);

    resolveLeave();
    return promise;
  });

  it("rolls back and shows a toast when the API call fails", async () => {
    useCalendarsStore.setState({ calendars: [personal] });
    vi.mocked(calendarsApi.leave).mockRejectedValue(new Error("network error"));

    await useCalendarsStore.getState().leaveCalendar("cal-1");

    expect(useCalendarsStore.getState().calendars).toEqual([personal]);
    expect(toast.error).toHaveBeenCalledWith("Failed to leave calendar.");
  });
});

describe("subscribeCalendar", () => {
  const subscribed = {
    id: "cal-3",
    name: "Team Holidays",
    color: "#8E44ADFF",
    sourceUrl: "https://example.com/feed.ics",
  };

  it("appends the calendar once the API call resolves", async () => {
    vi.mocked(calendarsApi.subscribe).mockResolvedValue(subscribed);
    vi.mocked(eventsApi.list).mockResolvedValue([]);

    const result = await useCalendarsStore
      .getState()
      .subscribeCalendar("https://example.com/feed.ics", "Team Holidays", "#8E44ADFF", true);

    expect(result).toEqual(subscribed);
    expect(useCalendarsStore.getState().calendars).toEqual([subscribed]);
    expect(calendarsApi.subscribe).toHaveBeenCalledWith("token-123", {
      url: "https://example.com/feed.ics",
      name: "Team Holidays",
      color: "#8E44ADFF",
      keepAlarms: true,
    });
  });

  // #93: the feed's Events already exist server-side by the time the
  // Subscribe response resolves — the store must re-fetch them itself so
  // the grid doesn't stay stale until a manual reload.
  it("re-fetches events once the subscribe succeeds", async () => {
    const imported = {
      id: "evt-9",
      calendarId: "cal-3",
      title: "Holiday",
      start: new Date("2026-08-10T00:00:00Z"),
      end: new Date("2026-08-11T00:00:00Z"),
    };
    vi.mocked(calendarsApi.subscribe).mockResolvedValue(subscribed);
    vi.mocked(eventsApi.list).mockResolvedValue([imported]);

    await useCalendarsStore
      .getState()
      .subscribeCalendar("https://example.com/feed.ics", "Team Holidays", "#8E44ADFF", true);

    expect(eventsApi.list).toHaveBeenCalledWith("token-123");
    expect(useEventsStore.getState().events).toHaveLength(1);
    expect(useEventsStore.getState().events[0].id).toBe("evt-9");
  });

  // A feed with no Events must still subscribe cleanly and leave the grid
  // unchanged, not error out because there's nothing new to show.
  it("subscribes to an empty feed without erroring", async () => {
    vi.mocked(calendarsApi.subscribe).mockResolvedValue(subscribed);
    vi.mocked(eventsApi.list).mockResolvedValue([]);

    await useCalendarsStore
      .getState()
      .subscribeCalendar("https://example.com/feed.ics", "Team Holidays", "#8E44ADFF", true);

    expect(useCalendarsStore.getState().calendars).toEqual([subscribed]);
    expect(useEventsStore.getState().events).toEqual([]);
  });

  it("does not add anything and rethrows when the API call fails", async () => {
    vi.mocked(calendarsApi.subscribe).mockRejectedValue(new Error("fetch failed"));

    await expect(
      useCalendarsStore
        .getState()
        .subscribeCalendar("https://example.com/feed.ics", "Team Holidays", "#8E44ADFF", false),
    ).rejects.toThrow("fetch failed");
    expect(useCalendarsStore.getState().calendars).toEqual([]);
    expect(eventsApi.list).not.toHaveBeenCalled();
    expect(useEventsStore.getState().events).toEqual([]);
  });

  // The subscribe itself already succeeded (the calendar is committed) by
  // the time the events re-fetch runs, so a failure there must not read as
  // a failed subscribe: it resolves, keeps the calendar, and toasts instead
  // of rethrowing through the Subscribe dialog's inline-error path.
  it("resolves and keeps the calendar when only the events re-fetch fails", async () => {
    vi.mocked(calendarsApi.subscribe).mockResolvedValue(subscribed);
    vi.mocked(eventsApi.list).mockRejectedValue(new Error("network error"));

    const result = await useCalendarsStore
      .getState()
      .subscribeCalendar("https://example.com/feed.ics", "Team Holidays", "#8E44ADFF", false);

    expect(result).toEqual(subscribed);
    expect(useCalendarsStore.getState().calendars).toEqual([subscribed]);
    expect(toast.error).toHaveBeenCalledWith(
      "Subscribed, but couldn't load its events. Reload to see them.",
    );
  });
});

describe("refreshCalendar", () => {
  const subscribed = {
    id: "cal-3",
    name: "Team Holidays",
    color: "#8E44ADFF",
    sourceUrl: "https://example.com/feed.ics",
  };

  it("refreshes then replaces the calendar with its updated lastSyncedAt", async () => {
    useCalendarsStore.setState({ calendars: [subscribed] });
    vi.mocked(calendarsApi.refresh).mockResolvedValue({
      notModified: false,
      created: 0,
      updated: 1,
      tombstoned: 0,
      unparseable: 0,
      noOp: 0,
    });
    const refreshed = { ...subscribed, lastSyncedAt: "2026-08-06T12:00:00Z" };
    vi.mocked(calendarsApi.get).mockResolvedValue(refreshed);
    vi.mocked(eventsApi.list).mockResolvedValue([]);

    await useCalendarsStore.getState().refreshCalendar("cal-3");

    expect(calendarsApi.refresh).toHaveBeenCalledWith("token-123", "cal-3");
    expect(calendarsApi.get).toHaveBeenCalledWith("token-123", "cal-3");
    expect(useCalendarsStore.getState().calendars).toEqual([refreshed]);
  });

  // #93: a Refresh that pulls in changed Events server-side must not leave
  // the grid showing the old ones.
  it("re-fetches events once the refresh succeeds", async () => {
    useCalendarsStore.setState({ calendars: [subscribed] });
    const updatedEvent = {
      id: "evt-9",
      calendarId: "cal-3",
      title: "Holiday (moved)",
      start: new Date("2026-08-12T00:00:00Z"),
      end: new Date("2026-08-13T00:00:00Z"),
    };
    vi.mocked(calendarsApi.refresh).mockResolvedValue({
      notModified: false,
      created: 0,
      updated: 1,
      tombstoned: 0,
      unparseable: 0,
      noOp: 0,
    });
    vi.mocked(calendarsApi.get).mockResolvedValue(subscribed);
    vi.mocked(eventsApi.list).mockResolvedValue([updatedEvent]);

    await useCalendarsStore.getState().refreshCalendar("cal-3");

    expect(eventsApi.list).toHaveBeenCalledWith("token-123");
    expect(useEventsStore.getState().events).toHaveLength(1);
    expect(useEventsStore.getState().events[0].title).toBe("Holiday (moved)");
  });

  it("rethrows and leaves the calendar and events untouched when the API call fails", async () => {
    useCalendarsStore.setState({ calendars: [subscribed] });
    vi.mocked(calendarsApi.refresh).mockRejectedValue(new Error("fetch failed"));

    await expect(
      useCalendarsStore.getState().refreshCalendar("cal-3"),
    ).rejects.toThrow("fetch failed");
    expect(useCalendarsStore.getState().calendars).toEqual([subscribed]);
    expect(calendarsApi.get).not.toHaveBeenCalled();
    expect(eventsApi.list).not.toHaveBeenCalled();
    expect(useEventsStore.getState().events).toEqual([]);
  });

  // Same isolation as subscribeCalendar: the refresh itself already
  // succeeded by the time the events re-fetch runs, so its failure must
  // not read as a failed refresh.
  it("resolves and keeps the refreshed calendar when only the events re-fetch fails", async () => {
    useCalendarsStore.setState({ calendars: [subscribed] });
    vi.mocked(calendarsApi.refresh).mockResolvedValue({
      notModified: false,
      created: 0,
      updated: 1,
      tombstoned: 0,
      unparseable: 0,
      noOp: 0,
    });
    const refreshed = { ...subscribed, lastSyncedAt: "2026-08-06T12:00:00Z" };
    vi.mocked(calendarsApi.get).mockResolvedValue(refreshed);
    vi.mocked(eventsApi.list).mockRejectedValue(new Error("network error"));

    await expect(
      useCalendarsStore.getState().refreshCalendar("cal-3"),
    ).resolves.toBeUndefined();

    expect(useCalendarsStore.getState().calendars).toEqual([refreshed]);
    expect(toast.error).toHaveBeenCalledWith(
      "Refreshed, but couldn't load its events. Reload to see them.",
    );
  });
});

// #126: the sidebar's share-count badge reads shareCount straight off the
// Calendar list, so a grant or revoke must pull a fresh list rather than
// adjust the count locally — see calendarsStore.ts for why optimistic
// adjustment was rejected.
describe("shareCalendar", () => {
  const share = {
    userId: 7,
    name: "bob",
    email: "bob@example.com",
    role: "viewer" as const,
    createdAt: "2026-08-06T12:00:00Z",
  };

  it("re-fetches calendars after granting a Share", async () => {
    useCalendarsStore.setState({ calendars: [personal] });
    vi.mocked(calendarsApi.share).mockResolvedValue(share);
    vi.mocked(calendarsApi.list).mockResolvedValue([{ ...personal, shareCount: 1 }]);

    const result = await useCalendarsStore
      .getState()
      .shareCalendar("cal-1", "bob", "viewer");

    expect(result).toEqual(share);
    expect(calendarsApi.share).toHaveBeenCalledWith("token-123", "cal-1", "bob", "viewer");
    expect(calendarsApi.list).toHaveBeenCalledWith("token-123");
    expect(useCalendarsStore.getState().calendars).toEqual([
      { ...personal, shareCount: 1 },
    ]);
  });

  it("rethrows and does not re-fetch when the grant fails", async () => {
    useCalendarsStore.setState({ calendars: [personal] });
    vi.mocked(calendarsApi.share).mockRejectedValue(new Error("already shared"));

    await expect(
      useCalendarsStore.getState().shareCalendar("cal-1", "bob", "viewer"),
    ).rejects.toThrow("already shared");
    expect(calendarsApi.list).not.toHaveBeenCalled();
  });
});

describe("revokeCalendarShare", () => {
  it("re-fetches calendars after revoking a Share", async () => {
    useCalendarsStore.setState({ calendars: [{ ...personal, shareCount: 1 }] });
    vi.mocked(calendarsApi.revokeShare).mockResolvedValue(undefined);
    vi.mocked(calendarsApi.list).mockResolvedValue([{ ...personal, shareCount: 0 }]);

    await useCalendarsStore.getState().revokeCalendarShare("cal-1", 7);

    expect(calendarsApi.revokeShare).toHaveBeenCalledWith("token-123", "cal-1", 7);
    expect(calendarsApi.list).toHaveBeenCalledWith("token-123");
    expect(useCalendarsStore.getState().calendars).toEqual([
      { ...personal, shareCount: 0 },
    ]);
  });

  it("rethrows and does not re-fetch when the revoke fails", async () => {
    useCalendarsStore.setState({ calendars: [{ ...personal, shareCount: 1 }] });
    vi.mocked(calendarsApi.revokeShare).mockRejectedValue(new Error("network error"));

    await expect(
      useCalendarsStore.getState().revokeCalendarShare("cal-1", 7),
    ).rejects.toThrow("network error");
    expect(calendarsApi.list).not.toHaveBeenCalled();
  });
});
