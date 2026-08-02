import { beforeEach, expect, it, vi } from "vitest";

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
const { useAuthStore } = await import("./authStore");
const { useCalendarsStore } = await import("./calendarsStore");
const { useEventsStore } = await import("./eventsStore");
const { useShellStore } = await import("./shellStore");
const { deleteCalendarCascade } = await import("./deleteCalendarCascade");

const personal = { id: "cal-1", name: "Personal", color: "peacock" as const };
const lunch = {
  id: "evt-1",
  calendarId: "cal-1",
  title: "Lunch",
  start: new Date("2026-01-01T12:00:00"),
  end: new Date("2026-01-01T13:00:00"),
  reminders: [],
};

beforeEach(() => {
  vi.clearAllMocks();
  useAuthStore.setState({ accessToken: "token-123" });
  useCalendarsStore.setState({ calendars: [personal] });
  useEventsStore.setState({ events: [lunch] });
  useShellStore.setState({ checkedCalendarIds: new Set(["cal-1"]) });
});

it("removes the calendar's events and checked state when the delete succeeds", async () => {
  vi.mocked(calendarsApi.remove).mockResolvedValue(undefined);

  await deleteCalendarCascade("cal-1");

  expect(useCalendarsStore.getState().calendars).toEqual([]);
  expect(useEventsStore.getState().events).toEqual([]);
  expect(useShellStore.getState().checkedCalendarIds.has("cal-1")).toBe(false);
});

it("restores events and checked state when the delete fails", async () => {
  vi.mocked(calendarsApi.remove).mockRejectedValue(new Error("network error"));

  await deleteCalendarCascade("cal-1");

  expect(useCalendarsStore.getState().calendars).toEqual([personal]);
  expect(useEventsStore.getState().events).toEqual([lunch]);
  expect(useShellStore.getState().checkedCalendarIds.has("cal-1")).toBe(true);
});
