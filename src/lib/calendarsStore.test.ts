import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("./calendarsApi", () => ({
  calendarsApi: {
    list: vi.fn(),
    create: vi.fn(),
    update: vi.fn(),
    remove: vi.fn(),
  },
}));

vi.mock("./toast", () => ({
  toast: { error: vi.fn() },
}));

const { calendarsApi } = await import("./calendarsApi");
const { toast } = await import("./toast");
const { useAuthStore } = await import("./authStore");
const { useCalendarsStore } = await import("./calendarsStore");

const personal = { id: "cal-1", name: "Personal", color: "peacock" as const };
const work = { id: "cal-2", name: "Work", color: "tomato" as const };

function resetStore() {
  useCalendarsStore.setState({ calendars: [] });
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
});
