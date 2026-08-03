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
  it('scope "this": creates a new override for an un-overridden Occurrence', async () => {
    useEventsStore.setState({ events: [recurringMaster] });
    vi.mocked(eventsApi.create).mockResolvedValue({} as never);

    const occurrenceStart = new Date("2026-01-03T09:00:00Z");
    await useEventsStore.getState().editOccurrence(
      occurrenceOf(recurringMaster, occurrenceStart),
      "this",
      {
        calendarId: "cal-1",
        title: "Standup (moved)",
        start: new Date("2026-01-03T10:00:00Z"),
        end: new Date("2026-01-03T10:30:00Z"),
        // The modal always includes the series' rrule in `changes`; the
        // created Override must never pick it up (ADR-0016) — a non-empty
        // rrule here is what the backend rejects with a 400.
        rrule: "FREQ=DAILY",
      },
    );

    const events = useEventsStore.getState().events;
    expect(events).toHaveLength(2);
    const override = events.find((e) => e.id !== "master-1")!;
    expect(override.parentId).toBe("master-1");
    expect(override.recurrenceId).toEqual(occurrenceStart);
    expect(override.title).toBe("Standup (moved)");
    expect(override.rrule).toBeUndefined();
    const createCall = vi.mocked(eventsApi.create).mock.calls[0][1];
    expect(createCall).toMatchObject({
      parentId: "master-1",
      recurrenceId: occurrenceStart,
    });
    expect(createCall).not.toHaveProperty("rrule");
  });

  it('scope "this": updates the override in place when the Occurrence is already one', async () => {
    const override = {
      id: "override-1",
      calendarId: "cal-1",
      title: "Standup (moved)",
      start: new Date("2026-01-03T10:00:00Z"),
      end: new Date("2026-01-03T10:30:00Z"),
      parentId: "master-1",
      recurrenceId: new Date("2026-01-03T09:00:00Z"),
    };
    useEventsStore.setState({ events: [recurringMaster, override] });
    vi.mocked(eventsApi.update).mockResolvedValue({} as never);

    await useEventsStore.getState().editOccurrence(
      occurrenceOf(override as never, override.start),
      "this",
      // The modal always includes the series' rrule in `changes` (for the
      // "all" scope's use); an Override must never pick it up.
      {
        calendarId: "cal-1",
        title: "Renamed again",
        start: override.start,
        end: override.end,
        rrule: "FREQ=DAILY",
      },
    );

    expect(eventsApi.update).toHaveBeenCalledWith(
      "token-123",
      "override-1",
      expect.objectContaining({ title: "Renamed again", rrule: undefined }),
    );
    const updatedOverride = useEventsStore.getState().events.find((e) => e.id === "override-1")!;
    expect(updatedOverride.title).toBe("Renamed again");
    expect(updatedOverride.rrule).toBeUndefined();
  });

  it('scope "all": updates the master and keeps overrides when the rule is unchanged', async () => {
    const override = {
      id: "override-1",
      calendarId: "cal-1",
      title: "Standup (moved)",
      start: new Date("2026-01-03T10:00:00Z"),
      end: new Date("2026-01-03T10:30:00Z"),
      parentId: "master-1",
      recurrenceId: new Date("2026-01-03T09:00:00Z"),
    };
    useEventsStore.setState({ events: [recurringMaster, override] });
    vi.mocked(eventsApi.update).mockResolvedValue({} as never);

    await useEventsStore.getState().editOccurrence(
      occurrenceOf(recurringMaster, recurringMaster.start),
      "all",
      { calendarId: "cal-1", title: "Renamed", start: recurringMaster.start, end: recurringMaster.end },
    );

    const events = useEventsStore.getState().events;
    expect(events.find((e) => e.id === "master-1")?.title).toBe("Renamed");
    expect(events.find((e) => e.id === "override-1")).toBeDefined();
  });

  it('scope "all": editing from a later Occurrence keeps the master anchored on its own date', async () => {
    useEventsStore.setState({ events: [recurringMaster] });
    vi.mocked(eventsApi.update).mockResolvedValue({} as never);

    // Opening the Jan 5th Occurrence and changing only the time must not
    // move the master's Jan 1 DTSTART — that would truncate every earlier
    // Occurrence out of the series.
    const openedOccurrenceStart = new Date("2026-01-05T09:00:00Z");
    await useEventsStore.getState().editOccurrence(
      occurrenceOf(recurringMaster, openedOccurrenceStart),
      "all",
      {
        calendarId: "cal-1",
        title: "Standup",
        start: new Date("2026-01-05T10:00:00Z"),
        end: new Date("2026-01-05T10:30:00Z"),
      },
    );

    const master = useEventsStore.getState().events.find((e) => e.id === "master-1")!;
    expect(master.start).toEqual(new Date("2026-01-01T10:00:00Z"));
    expect(master.end).toEqual(new Date("2026-01-01T10:30:00Z"));
  });

  it('scope "all": discards existing overrides when the rule changes', async () => {
    const override = {
      id: "override-1",
      calendarId: "cal-1",
      title: "Standup (moved)",
      start: new Date("2026-01-03T10:00:00Z"),
      end: new Date("2026-01-03T10:30:00Z"),
      parentId: "master-1",
      recurrenceId: new Date("2026-01-03T09:00:00Z"),
    };
    useEventsStore.setState({ events: [recurringMaster, override] });
    vi.mocked(eventsApi.update).mockResolvedValue({} as never);

    await useEventsStore.getState().editOccurrence(
      occurrenceOf(recurringMaster, recurringMaster.start),
      "all",
      {
        calendarId: "cal-1",
        title: "Standup",
        start: recurringMaster.start,
        end: recurringMaster.end,
        rrule: "FREQ=WEEKLY;BYDAY=TH",
      },
    );

    const events = useEventsStore.getState().events;
    expect(events).toHaveLength(1);
    expect(events[0].rrule).toBe("FREQ=WEEKLY;BYDAY=TH");
  });

  it('scope "all": clears the rule entirely when the user picks "Does not repeat"', async () => {
    useEventsStore.setState({ events: [recurringMaster] });
    vi.mocked(eventsApi.update).mockResolvedValue({} as never);

    // `rrule: undefined` here is a deliberate "does not repeat", distinct
    // from omitting the field — the store must not fall back to keeping the
    // old rule just because the new value happens to be undefined.
    await useEventsStore.getState().editOccurrence(
      occurrenceOf(recurringMaster, recurringMaster.start),
      "all",
      {
        calendarId: "cal-1",
        title: "Standup",
        start: recurringMaster.start,
        end: recurringMaster.end,
        rrule: undefined,
      },
    );

    const master = useEventsStore.getState().events.find((e) => e.id === "master-1")!;
    expect(master.rrule).toBeUndefined();
    expect(eventsApi.update).toHaveBeenCalledWith(
      "token-123",
      "master-1",
      expect.objectContaining({ rrule: undefined }),
    );
  });

  it('scope "all": a drag to a different day shifts the whole series and reanchors the rule', async () => {
    useEventsStore.setState({ events: [recurringMaster] });
    vi.mocked(eventsApi.update).mockResolvedValue({} as never);

    // recurringMaster is FREQ=DAILY (weekday-agnostic), so use a weekly rule
    // to exercise the day-changed reanchor path explicitly.
    const weeklyMaster = { ...recurringMaster, rrule: "FREQ=WEEKLY;BYDAY=TH" };
    useEventsStore.setState({ events: [weeklyMaster] });

    // Jan 1 2026 is a Thursday; drag it one day later to a Friday.
    await useEventsStore.getState().editOccurrence(
      occurrenceOf(weeklyMaster, weeklyMaster.start),
      "all",
      {
        calendarId: "cal-1",
        title: "Standup",
        start: new Date("2026-01-02T09:00:00Z"),
        end: new Date("2026-01-02T09:30:00Z"),
      },
    );

    const master = useEventsStore.getState().events.find((e) => e.id === "master-1")!;
    expect(master.start).toEqual(new Date("2026-01-02T09:00:00Z"));
    expect(master.rrule).toBe("FREQ=WEEKLY;BYDAY=FR");
  });

  it('scope "following": truncates the old master and creates a new one for the remainder', async () => {
    useEventsStore.setState({ events: [recurringMaster] });
    vi.mocked(eventsApi.update).mockResolvedValue({} as never);
    vi.mocked(eventsApi.create).mockResolvedValue({} as never);
    vi.mocked(eventsApi.reparentSeries).mockResolvedValue(undefined);

    const splitStart = new Date("2026-01-03T09:00:00Z");
    await useEventsStore.getState().editOccurrence(
      occurrenceOf(recurringMaster, splitStart),
      "following",
      {
        calendarId: "cal-1",
        title: "Standup (renamed)",
        start: new Date("2026-01-03T10:00:00Z"),
        end: new Date("2026-01-03T10:30:00Z"),
      },
    );

    const events = useEventsStore.getState().events;
    expect(events).toHaveLength(2);
    const oldMaster = events.find((e) => e.id === "master-1")!;
    expect(oldMaster.rrule).toContain("UNTIL=20260102");
    const newMaster = events.find((e) => e.id !== "master-1")!;
    expect(newMaster.title).toBe("Standup (renamed)");
    expect(newMaster.rrule).toBe("FREQ=DAILY");
    expect(eventsApi.reparentSeries).toHaveBeenCalledWith(
      "token-123",
      "master-1",
      newMaster.id,
      splitStart,
    );
  });
});

describe("deleteOccurrence", () => {
  it('scope "this": adds an exception for an un-overridden Occurrence', async () => {
    useEventsStore.setState({ events: [recurringMaster] });
    vi.mocked(eventsApi.addException).mockResolvedValue(undefined);

    const occurrenceStart = new Date("2026-01-03T09:00:00Z");
    await useEventsStore
      .getState()
      .deleteOccurrence(occurrenceOf(recurringMaster, occurrenceStart), "this");

    expect(eventsApi.addException).toHaveBeenCalledWith(
      "token-123",
      "master-1",
      occurrenceStart,
    );
    expect(useEventsStore.getState().events[0].exdates).toEqual([occurrenceStart]);
  });

  it('scope "this": deletes the override outright when the Occurrence is already one', async () => {
    const override = {
      id: "override-1",
      calendarId: "cal-1",
      title: "Standup (moved)",
      start: new Date("2026-01-03T10:00:00Z"),
      end: new Date("2026-01-03T10:30:00Z"),
      parentId: "master-1",
      recurrenceId: new Date("2026-01-03T09:00:00Z"),
    };
    useEventsStore.setState({ events: [recurringMaster, override] });
    vi.mocked(eventsApi.remove).mockResolvedValue(undefined);

    await useEventsStore
      .getState()
      .deleteOccurrence(occurrenceOf(override as never, override.start), "this");

    expect(eventsApi.remove).toHaveBeenCalledWith("token-123", "override-1");
    expect(useEventsStore.getState().events.map((e) => e.id)).toEqual(["master-1"]);
  });

  it('scope "all": deletes the master and any local overrides', async () => {
    const override = {
      id: "override-1",
      calendarId: "cal-1",
      title: "Standup (moved)",
      start: new Date("2026-01-03T10:00:00Z"),
      end: new Date("2026-01-03T10:30:00Z"),
      parentId: "master-1",
      recurrenceId: new Date("2026-01-03T09:00:00Z"),
    };
    useEventsStore.setState({ events: [recurringMaster, override] });
    vi.mocked(eventsApi.remove).mockResolvedValue(undefined);

    await useEventsStore
      .getState()
      .deleteOccurrence(occurrenceOf(recurringMaster, recurringMaster.start), "all");

    expect(useEventsStore.getState().events).toEqual([]);
  });

  it('scope "following": truncates the master and deletes overrides at-or-after the split', async () => {
    const overrideBefore = {
      id: "override-before",
      calendarId: "cal-1",
      title: "Before",
      start: new Date("2026-01-02T10:00:00Z"),
      end: new Date("2026-01-02T10:30:00Z"),
      parentId: "master-1",
      recurrenceId: new Date("2026-01-02T09:00:00Z"),
    };
    const overrideAfter = {
      id: "override-after",
      calendarId: "cal-1",
      title: "After",
      start: new Date("2026-01-04T10:00:00Z"),
      end: new Date("2026-01-04T10:30:00Z"),
      parentId: "master-1",
      recurrenceId: new Date("2026-01-04T09:00:00Z"),
    };
    useEventsStore.setState({
      events: [recurringMaster, overrideBefore, overrideAfter],
    });
    vi.mocked(eventsApi.update).mockResolvedValue({} as never);
    vi.mocked(eventsApi.remove).mockResolvedValue(undefined);

    const splitStart = new Date("2026-01-03T09:00:00Z");
    await useEventsStore
      .getState()
      .deleteOccurrence(occurrenceOf(recurringMaster, splitStart), "following");

    const events = useEventsStore.getState().events;
    expect(events.map((e) => e.id).sort()).toEqual(["master-1", "override-before"]);
    expect(events.find((e) => e.id === "master-1")?.rrule).toContain("UNTIL=20260102");
    expect(eventsApi.remove).toHaveBeenCalledWith("token-123", "override-after");
    expect(eventsApi.remove).not.toHaveBeenCalledWith("token-123", "override-before");
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
