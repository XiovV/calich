import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("./eventsApi", () => ({
  eventsApi: {
    list: vi.fn(),
    create: vi.fn(),
    update: vi.fn(),
    remove: vi.fn(),
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

describe("removeEventsByCalendarId", () => {
  it("removes all events belonging to the calendar without calling the API", () => {
    const other = { ...standup, id: "evt-2", calendarId: "cal-2" };
    useEventsStore.setState({ events: [standup, other] });

    useEventsStore.getState().removeEventsByCalendarId("cal-1");

    expect(useEventsStore.getState().events).toEqual([other]);
    expect(eventsApi.remove).not.toHaveBeenCalled();
  });
});
