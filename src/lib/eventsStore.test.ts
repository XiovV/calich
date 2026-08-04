import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("./eventsApi", () => ({
  eventsApi: {
    list: vi.fn(),
    create: vi.fn(),
    update: vi.fn(),
    remove: vi.fn(),
    addException: vi.fn(),
    reparentSeries: vi.fn(),
  },
}));

vi.mock("./toast", () => ({
  toast: { error: vi.fn() },
}));

const { eventsApi } = await import("./eventsApi");
const { toast } = await import("./toast");
const { useAuthStore } = await import("./authStore");
const { useEventsStore } = await import("./eventsStore");

const standup = {
  id: "evt-1",
  calendarId: "cal-1",
  title: "Standup",
  start: new Date("2026-01-01T09:00:00Z"),
  end: new Date("2026-01-01T10:00:00Z"),
};

function resetStore() {
  useEventsStore.setState({ events: [] });
  useAuthStore.setState({ accessToken: "token-123" });
}

beforeEach(() => {
  vi.clearAllMocks();
  resetStore();
});

describe("fetchEvents", () => {
  it("loads events from the API", async () => {
    vi.mocked(eventsApi.list).mockResolvedValue([standup]);

    await useEventsStore.getState().fetchEvents();

    expect(useEventsStore.getState().events).toEqual([standup]);
    expect(eventsApi.list).toHaveBeenCalledWith("token-123");
  });
});

describe("addEvent", () => {
  it("adds the event immediately, before the API call resolves", () => {
    let resolveCreate: () => void = () => {};
    vi.mocked(eventsApi.create).mockReturnValue(
      new Promise((resolve) => {
        resolveCreate = () => resolve(standup);
      }),
    );

    const promise = useEventsStore.getState().addEvent(standup);

    expect(useEventsStore.getState().events).toEqual([standup]);

    resolveCreate();
    return promise;
  });

  it("rolls back and shows a toast when the API call fails", async () => {
    vi.mocked(eventsApi.create).mockRejectedValue(new Error("network error"));

    await useEventsStore.getState().addEvent(standup);

    expect(useEventsStore.getState().events).toEqual([]);
    expect(toast.error).toHaveBeenCalledWith('Failed to create event "Standup".');
  });
});

describe("updateEvent", () => {
  it("merges partial changes into the full event immediately, and sends the full payload", () => {
    useEventsStore.setState({ events: [standup] });
    let resolveUpdate: () => void = () => {};
    vi.mocked(eventsApi.update).mockReturnValue(
      new Promise((resolve) => {
        resolveUpdate = () => resolve({ ...standup, title: "Renamed" });
      }),
    );

    const promise = useEventsStore.getState().updateEvent("evt-1", { title: "Renamed" });

    expect(useEventsStore.getState().events[0].title).toBe("Renamed");
    expect(eventsApi.update).toHaveBeenCalledWith("token-123", "evt-1", {
      calendarId: standup.calendarId,
      title: "Renamed",
      start: standup.start,
      end: standup.end,
    });

    resolveUpdate();
    return promise;
  });

  it("rolls back and shows a toast when the API call fails", async () => {
    useEventsStore.setState({ events: [standup] });
    vi.mocked(eventsApi.update).mockRejectedValue(new Error("network error"));

    await useEventsStore.getState().updateEvent("evt-1", { title: "Renamed" });

    expect(useEventsStore.getState().events).toEqual([standup]);
    expect(toast.error).toHaveBeenCalledWith("Failed to update event.");
  });

  it("is a no-op for an id that isn't in the store", async () => {
    await useEventsStore.getState().updateEvent("does-not-exist", { title: "Renamed" });

    expect(eventsApi.update).not.toHaveBeenCalled();
  });
});

describe("removeEvent", () => {
  it("removes the event immediately", () => {
    useEventsStore.setState({ events: [standup] });
    let resolveRemove: () => void = () => {};
    vi.mocked(eventsApi.remove).mockReturnValue(
      new Promise((resolve) => {
        resolveRemove = () => resolve(undefined);
      }),
    );

    const promise = useEventsStore.getState().removeEvent("evt-1");

    expect(useEventsStore.getState().events).toEqual([]);

    resolveRemove();
    return promise;
  });

  it("rolls back and shows a toast when the API call fails", async () => {
    useEventsStore.setState({ events: [standup] });
    vi.mocked(eventsApi.remove).mockRejectedValue(new Error("network error"));

    await useEventsStore.getState().removeEvent("evt-1");

    expect(useEventsStore.getState().events).toEqual([standup]);
    expect(toast.error).toHaveBeenCalledWith("Failed to delete event.");
  });
});

const recurringMaster = {
  id: "master-1",
  calendarId: "cal-1",
  title: "Standup",
  start: new Date("2026-01-01T09:00:00Z"),
  end: new Date("2026-01-01T09:30:00Z"),
  rrule: "FREQ=DAILY",
};

function occurrenceOf(event: typeof recurringMaster, start: Date) {
  const durationMs = event.end.getTime() - event.start.getTime();
  return { event, start, end: new Date(start.getTime() + durationMs) };
}

describe("editOccurrence", () => {
  it("applies the planned change to the cache immediately, before the API call resolves", () => {
    useEventsStore.setState({ events: [recurringMaster] });
    let resolveCreate: () => void = () => {};
    vi.mocked(eventsApi.create).mockReturnValue(
      new Promise((resolve) => {
        resolveCreate = () => resolve({} as never);
      }),
    );

    const occurrenceStart = new Date("2026-01-03T09:00:00Z");
    const promise = useEventsStore.getState().editOccurrence(
      occurrenceOf(recurringMaster, occurrenceStart),
      "this",
      {
        calendarId: "cal-1",
        title: "Standup (moved)",
        start: new Date("2026-01-03T10:00:00Z"),
        end: new Date("2026-01-03T10:30:00Z"),
        rrule: "FREQ=DAILY",
      },
    );

    expect(useEventsStore.getState().events).toHaveLength(2);

    resolveCreate();
    return promise;
  });

  it("rolls back the whole cache and shows a toast when the dispatch fails", async () => {
    useEventsStore.setState({ events: [recurringMaster] });
    vi.mocked(eventsApi.create).mockRejectedValue(new Error("network error"));

    await useEventsStore.getState().editOccurrence(
      occurrenceOf(recurringMaster, new Date("2026-01-03T09:00:00Z")),
      "this",
      {
        calendarId: "cal-1",
        title: "Standup (moved)",
        start: new Date("2026-01-03T10:00:00Z"),
        end: new Date("2026-01-03T10:30:00Z"),
      },
    );

    expect(useEventsStore.getState().events).toEqual([recurringMaster]);
    expect(toast.error).toHaveBeenCalledWith("Failed to update event.");
  });

  it('rolls back all local mutations when a later call in a "following" dispatch fails', async () => {
    useEventsStore.setState({ events: [recurringMaster] });
    vi.mocked(eventsApi.update).mockResolvedValue({} as never);
    vi.mocked(eventsApi.create).mockResolvedValue({} as never);
    vi.mocked(eventsApi.reparentSeries).mockRejectedValue(new Error("network error"));

    await useEventsStore.getState().editOccurrence(
      occurrenceOf(recurringMaster, new Date("2026-01-03T09:00:00Z")),
      "following",
      {
        calendarId: "cal-1",
        title: "Standup (renamed)",
        start: new Date("2026-01-03T10:00:00Z"),
        end: new Date("2026-01-03T10:30:00Z"),
      },
    );

    expect(useEventsStore.getState().events).toEqual([recurringMaster]);
    expect(toast.error).toHaveBeenCalledWith("Failed to update event.");
  });
});

describe("deleteOccurrence", () => {
  it("applies the planned change to the cache immediately, before the API call resolves", () => {
    useEventsStore.setState({ events: [recurringMaster] });
    let resolveAddException: () => void = () => {};
    vi.mocked(eventsApi.addException).mockReturnValue(
      new Promise((resolve) => {
        resolveAddException = () => resolve(undefined);
      }),
    );

    const occurrenceStart = new Date("2026-01-03T09:00:00Z");
    const promise = useEventsStore
      .getState()
      .deleteOccurrence(occurrenceOf(recurringMaster, occurrenceStart), "this");

    expect(useEventsStore.getState().events[0].exdates).toEqual([occurrenceStart]);

    resolveAddException();
    return promise;
  });

  it("rolls back the whole cache and shows a toast when the dispatch fails", async () => {
    useEventsStore.setState({ events: [recurringMaster] });
    vi.mocked(eventsApi.addException).mockRejectedValue(new Error("network error"));

    await useEventsStore
      .getState()
      .deleteOccurrence(occurrenceOf(recurringMaster, new Date("2026-01-03T09:00:00Z")), "this");

    expect(useEventsStore.getState().events).toEqual([recurringMaster]);
    expect(toast.error).toHaveBeenCalledWith("Failed to delete event.");
  });

  it('rolls back all local mutations when a later call in a "following" dispatch fails', async () => {
    const overrideAfter = {
      id: "override-after",
      calendarId: "cal-1",
      title: "After",
      start: new Date("2026-01-04T10:00:00Z"),
      end: new Date("2026-01-04T10:30:00Z"),
      parentId: "master-1",
      recurrenceId: new Date("2026-01-04T09:00:00Z"),
    };
    useEventsStore.setState({ events: [recurringMaster, overrideAfter] });
    vi.mocked(eventsApi.update).mockResolvedValue({} as never);
    vi.mocked(eventsApi.remove).mockRejectedValue(new Error("network error"));

    await useEventsStore
      .getState()
      .deleteOccurrence(occurrenceOf(recurringMaster, new Date("2026-01-03T09:00:00Z")), "following");

    expect(useEventsStore.getState().events).toEqual([recurringMaster, overrideAfter]);
    expect(toast.error).toHaveBeenCalledWith("Failed to delete event.");
  });
});

describe("removeEventsByCalendarId", () => {
  it("removes all events belonging to the calendar without calling the API", () => {
    const other = { ...standup, id: "evt-2", calendarId: "cal-2" };
    useEventsStore.setState({ events: [standup, other] });

    useEventsStore.getState().removeEventsByCalendarId("cal-1");

    expect(useEventsStore.getState().events).toEqual([other]);
    expect(eventsApi.remove).not.toHaveBeenCalled();
  });
});
