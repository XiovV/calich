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

vi.mock("./calendarsApi", () => ({
  calendarsApi: {
    list: vi.fn(),
  },
}));

vi.mock("./toast", () => ({
  toast: { error: vi.fn() },
}));

const { eventsApi } = await import("./eventsApi");
const { calendarsApi } = await import("./calendarsApi");
const { toast } = await import("./toast");
const { ApiError } = await import("./apiClient");
const { useAuthStore } = await import("./authStore");
const { useCalendarsStore } = await import("./calendarsStore");
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
  useCalendarsStore.setState({ calendars: [] });
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

  it("persists Reminders authored on a new event (ADR-0020)", async () => {
    const reminders = [{ offsetMinutes: 10, channel: "notification" as const }];
    const remindedStandup = { ...standup, reminders };
    vi.mocked(eventsApi.create).mockResolvedValue(remindedStandup);

    await useEventsStore.getState().addEvent(remindedStandup);

    expect(useEventsStore.getState().events[0].reminders).toEqual(reminders);
    expect(eventsApi.create).toHaveBeenCalledWith("token-123", remindedStandup);
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

  it("preserves the event's own tzid when changes don't touch it (ADR-0019)", () => {
    const zonedStandup = { ...standup, tzid: "Europe/Berlin" };
    useEventsStore.setState({ events: [zonedStandup] });
    vi.mocked(eventsApi.update).mockResolvedValue({ ...zonedStandup, title: "Renamed" });

    const promise = useEventsStore.getState().updateEvent("evt-1", { title: "Renamed" });

    expect(eventsApi.update).toHaveBeenCalledWith(
      "token-123",
      "evt-1",
      expect.objectContaining({ tzid: "Europe/Berlin" }),
    );
    return promise;
  });

  it("is a no-op for an id that isn't in the store", async () => {
    await useEventsStore.getState().updateEvent("does-not-exist", { title: "Renamed" });

    expect(eventsApi.update).not.toHaveBeenCalled();
  });

  it("round-trips reminders through a local update (ADR-0020)", () => {
    const reminders = [{ offsetMinutes: 10, channel: "notification" as const }];
    const remindedStandup = { ...standup, reminders };
    useEventsStore.setState({ events: [remindedStandup] });
    vi.mocked(eventsApi.update).mockResolvedValue(remindedStandup);

    const promise = useEventsStore.getState().updateEvent("evt-1", { title: "Renamed" });

    expect(eventsApi.update).toHaveBeenCalledWith(
      "token-123",
      "evt-1",
      expect.objectContaining({ reminders }),
    );
    return promise;
  });

  it("persists Reminders newly authored on an existing event", () => {
    const reminders = [
      { offsetMinutes: 5, channel: "notification" as const },
      { offsetMinutes: 1440, channel: "email" as const },
    ];
    useEventsStore.setState({ events: [standup] });
    vi.mocked(eventsApi.update).mockResolvedValue({ ...standup, reminders });

    const promise = useEventsStore.getState().updateEvent("evt-1", { reminders });

    expect(useEventsStore.getState().events[0].reminders).toEqual(reminders);
    expect(eventsApi.update).toHaveBeenCalledWith(
      "token-123",
      "evt-1",
      expect.objectContaining({ reminders }),
    );
    return promise;
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

  it('scope "this": a new Override starts with a copy of the Master\'s Reminders (ADR-0020)', async () => {
    const reminders = [{ offsetMinutes: 10, channel: "notification" as const }];
    const remindedMaster = { ...recurringMaster, reminders };
    useEventsStore.setState({ events: [remindedMaster] });
    vi.mocked(eventsApi.create).mockResolvedValue({} as never);

    const occurrenceStart = new Date("2026-01-03T09:00:00Z");
    await useEventsStore.getState().editOccurrence(
      occurrenceOf(remindedMaster, occurrenceStart),
      "this",
      {
        calendarId: "cal-1",
        title: "Standup (moved)",
        start: new Date("2026-01-03T10:00:00Z"),
        end: new Date("2026-01-03T10:30:00Z"),
      },
    );

    const override = useEventsStore.getState().events.find((e) => e.parentId === "master-1");
    expect(override?.reminders).toEqual(reminders);
    expect(eventsApi.create).toHaveBeenCalledWith(
      "token-123",
      expect.objectContaining({ reminders }),
    );
  });

  it('scope "all": authored Reminders replace the Master\'s wholesale', async () => {
    const remindedMaster = {
      ...recurringMaster,
      reminders: [{ offsetMinutes: 10, channel: "notification" as const }],
    };
    useEventsStore.setState({ events: [remindedMaster] });
    vi.mocked(eventsApi.update).mockResolvedValue({} as never);
    const authoredReminders = [{ offsetMinutes: 30, channel: "email" as const }];

    await useEventsStore.getState().editOccurrence(
      occurrenceOf(remindedMaster, remindedMaster.start),
      "all",
      {
        calendarId: "cal-1",
        title: "Standup",
        start: remindedMaster.start,
        end: remindedMaster.end,
        reminders: authoredReminders,
      },
    );

    expect(useEventsStore.getState().events[0].reminders).toEqual(authoredReminders);
    expect(eventsApi.update).toHaveBeenCalledWith(
      "token-123",
      "master-1",
      expect.objectContaining({ reminders: authoredReminders }),
    );
  });

  it('scope "this" on an existing Override: authored Reminders touch only that row, not the Master (ADR-0020)', async () => {
    const masterReminders = [{ offsetMinutes: 10, channel: "notification" as const }];
    const remindedMaster = { ...recurringMaster, reminders: masterReminders };
    const override = {
      id: "override-1",
      calendarId: "cal-1",
      title: "Standup (moved)",
      start: new Date("2026-01-03T10:00:00Z"),
      end: new Date("2026-01-03T10:30:00Z"),
      parentId: "master-1",
      recurrenceId: new Date("2026-01-03T09:00:00Z"),
      reminders: masterReminders,
    };
    useEventsStore.setState({ events: [remindedMaster, override] });
    vi.mocked(eventsApi.update).mockResolvedValue({} as never);
    const authoredReminders = [{ offsetMinutes: 30, channel: "email" as const }];

    await useEventsStore.getState().editOccurrence(
      occurrenceOf(override as never, override.start),
      "this",
      {
        calendarId: "cal-1",
        title: "Renamed again",
        start: override.start,
        end: override.end,
        reminders: authoredReminders,
      },
    );

    const events = useEventsStore.getState().events;
    expect(events.find((e) => e.id === "override-1")?.reminders).toEqual(authoredReminders);
    expect(events.find((e) => e.id === "master-1")?.reminders).toEqual(masterReminders);
  });

  it('scope "following": the new master carries the old master\'s Reminders forward', async () => {
    const reminders = [{ offsetMinutes: 10, channel: "notification" as const }];
    const remindedMaster = { ...recurringMaster, reminders };
    useEventsStore.setState({ events: [remindedMaster] });
    vi.mocked(eventsApi.update).mockResolvedValue({} as never);
    vi.mocked(eventsApi.create).mockResolvedValue({} as never);
    vi.mocked(eventsApi.reparentSeries).mockResolvedValue(undefined);

    const splitStart = new Date("2026-01-03T09:00:00Z");
    await useEventsStore.getState().editOccurrence(
      occurrenceOf(remindedMaster, splitStart),
      "following",
      {
        calendarId: "cal-1",
        title: "Standup (renamed)",
        start: new Date("2026-01-03T10:00:00Z"),
        end: new Date("2026-01-03T10:30:00Z"),
      },
    );

    const newMaster = useEventsStore
      .getState()
      .events.find((e) => e.id !== "master-1" && e.rrule);
    expect(newMaster?.reminders).toEqual(reminders);
    // The old (truncated) master's own Reminders are preserved on the wire too
    // — the update API replaces Reminders wholesale on any omitted field.
    expect(eventsApi.update).toHaveBeenCalledWith(
      "token-123",
      "master-1",
      expect.objectContaining({ reminders }),
    );
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

describe("recovery from a write refused because Access changed (#116)", () => {
  it("refetches Calendars and names the Calendar in the toast on a 403 from updateEvent", async () => {
    useEventsStore.setState({ events: [standup] });
    useCalendarsStore.setState({ calendars: [{ id: "cal-1", name: "Family", color: "#fff" }] });
    vi.mocked(eventsApi.update).mockRejectedValue(
      new ApiError(403, "forbidden", "calendar is read-only"),
    );
    vi.mocked(calendarsApi.list).mockResolvedValue([
      { id: "cal-1", name: "Family", color: "#fff", access: "viewer" },
    ]);

    await useEventsStore.getState().updateEvent("evt-1", { title: "Renamed" });

    expect(useEventsStore.getState().events).toEqual([standup]);
    expect(calendarsApi.list).toHaveBeenCalledWith("token-123");
    expect(useCalendarsStore.getState().calendars).toEqual([
      { id: "cal-1", name: "Family", color: "#fff", access: "viewer" },
    ]);
    expect(toast.error).toHaveBeenCalledWith(
      'Your access to "Family" has changed. Refreshing calendars.',
    );
  });

  it("refetches Calendars on a 404 from removeEvent", async () => {
    useEventsStore.setState({ events: [standup] });
    useCalendarsStore.setState({ calendars: [{ id: "cal-1", name: "Family", color: "#fff" }] });
    vi.mocked(eventsApi.remove).mockRejectedValue(
      new ApiError(404, "not_found", "event not found"),
    );
    vi.mocked(calendarsApi.list).mockResolvedValue([]);

    await useEventsStore.getState().removeEvent("evt-1");

    expect(useEventsStore.getState().events).toEqual([standup]);
    expect(calendarsApi.list).toHaveBeenCalledWith("token-123");
    expect(toast.error).toHaveBeenCalledWith(
      'Your access to "Family" has changed. Refreshing calendars.',
    );
  });

  it("falls back to the generic toast for a non-access failure, without refetching Calendars", async () => {
    useEventsStore.setState({ events: [standup] });
    vi.mocked(eventsApi.update).mockRejectedValue(new Error("network error"));

    await useEventsStore.getState().updateEvent("evt-1", { title: "Renamed" });

    expect(calendarsApi.list).not.toHaveBeenCalled();
    expect(toast.error).toHaveBeenCalledWith("Failed to update event.");
  });

  it("refetches Calendars and names the Calendar in the toast on a 403 from addEvent", async () => {
    useEventsStore.setState({ events: [] });
    useCalendarsStore.setState({ calendars: [{ id: "cal-1", name: "Family", color: "#fff" }] });
    vi.mocked(eventsApi.create).mockRejectedValue(
      new ApiError(403, "forbidden", "calendar is read-only"),
    );
    vi.mocked(calendarsApi.list).mockResolvedValue([
      { id: "cal-1", name: "Family", color: "#fff", access: "viewer" },
    ]);

    await useEventsStore.getState().addEvent(standup);

    expect(useEventsStore.getState().events).toEqual([]);
    expect(calendarsApi.list).toHaveBeenCalledWith("token-123");
    expect(toast.error).toHaveBeenCalledWith(
      'Your access to "Family" has changed. Refreshing calendars.',
    );
  });

  it("refetches Calendars on a 403 dispatch failure from editOccurrence", async () => {
    useEventsStore.setState({ events: [recurringMaster] });
    useCalendarsStore.setState({ calendars: [{ id: "cal-1", name: "Family", color: "#fff" }] });
    vi.mocked(eventsApi.create).mockRejectedValue(
      new ApiError(403, "forbidden", "calendar is read-only"),
    );
    vi.mocked(calendarsApi.list).mockResolvedValue([
      { id: "cal-1", name: "Family", color: "#fff", access: "viewer" },
    ]);

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
    expect(calendarsApi.list).toHaveBeenCalledWith("token-123");
    expect(toast.error).toHaveBeenCalledWith(
      'Your access to "Family" has changed. Refreshing calendars.',
    );
  });

  it("refetches Calendars on a 404 dispatch failure from deleteOccurrence", async () => {
    useEventsStore.setState({ events: [recurringMaster] });
    useCalendarsStore.setState({ calendars: [{ id: "cal-1", name: "Family", color: "#fff" }] });
    vi.mocked(eventsApi.addException).mockRejectedValue(
      new ApiError(404, "not_found", "event not found"),
    );
    vi.mocked(calendarsApi.list).mockResolvedValue([]);

    await useEventsStore
      .getState()
      .deleteOccurrence(occurrenceOf(recurringMaster, new Date("2026-01-03T09:00:00Z")), "this");

    expect(useEventsStore.getState().events).toEqual([recurringMaster]);
    expect(calendarsApi.list).toHaveBeenCalledWith("token-123");
    expect(toast.error).toHaveBeenCalledWith(
      'Your access to "Family" has changed. Refreshing calendars.',
    );
  });
});
