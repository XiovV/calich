import { beforeEach, expect, it, vi } from "vitest";

vi.mock("./calendarsApi", () => ({
  calendarsApi: {
    list: vi.fn(),
    create: vi.fn(),
    update: vi.fn(),
    remove: vi.fn(),
    leave: vi.fn(),
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
const { leaveCalendarCascade } = await import("./leaveCalendarCascade");

const shared = {
  id: "cal-1",
  name: "Family",
  color: "peacock" as const,
  isOwner: false,
  ownerUsername: "alice",
};
const lunch = {
  id: "evt-1",
  calendarId: "cal-1",
  title: "Lunch",
  start: new Date("2026-01-01T12:00:00"),
  end: new Date("2026-01-01T13:00:00"),
};

beforeEach(() => {
  vi.clearAllMocks();
  useAuthStore.setState({ accessToken: "token-123" });
  useCalendarsStore.setState({ calendars: [shared] });
  useEventsStore.setState({ events: [lunch] });
  useShellStore.setState({ checkedCalendarIds: new Set(["cal-1"]) });
});

it("removes the calendar's events and checked state when the leave succeeds", async () => {
  vi.mocked(calendarsApi.leave).mockResolvedValue(undefined);

  await leaveCalendarCascade("cal-1");

  expect(useCalendarsStore.getState().calendars).toEqual([]);
  expect(useEventsStore.getState().events).toEqual([]);
  expect(useShellStore.getState().checkedCalendarIds.has("cal-1")).toBe(false);
});

it("restores events and checked state when the leave fails", async () => {
  vi.mocked(calendarsApi.leave).mockRejectedValue(new Error("network error"));

  await leaveCalendarCascade("cal-1");

  expect(useCalendarsStore.getState().calendars).toEqual([shared]);
  expect(useEventsStore.getState().events).toEqual([lunch]);
  expect(useShellStore.getState().checkedCalendarIds.has("cal-1")).toBe(true);
});
