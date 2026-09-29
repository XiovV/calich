import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("./calendarSetsApi", async () => {
  const actual = await vi.importActual<typeof import("./calendarSetsApi")>("./calendarSetsApi");
  return {
    ...actual,
    calendarSetsApi: { ...actual.calendarSetsApi, remove: vi.fn() },
  };
});

const { calendarSetsApi } = await import("./calendarSetsApi");
const { useCalendarSetsStore } = await import("./calendarSetsStore");
const { useShellStore } = await import("./shellStore");
const { useAuthStore } = await import("./authStore");

const workSet = { id: 1, name: "Work", calendarIds: [] };
const weekendSet = { id: 2, name: "Weekend", calendarIds: [] };

beforeEach(() => {
  vi.clearAllMocks();
  useAuthStore.setState({ accessToken: "token-123" });
  useCalendarSetsStore.setState({ calendarSets: [workSet, weekendSet] });
  useShellStore.setState({ activeCalendarSetId: null });
});

// #304, ADR-0082: deleting the Set currently active falls back to "All
// calendars" rather than leaving the caller looking through something that
// no longer exists.
describe("deleteCalendarSet", () => {
  it("resets the Active Calendar Set to All calendars when the deleted Set was active", async () => {
    vi.mocked(calendarSetsApi.remove).mockResolvedValue(undefined);
    useShellStore.setState({ activeCalendarSetId: workSet.id });

    await useCalendarSetsStore.getState().deleteCalendarSet(workSet.id);

    expect(useShellStore.getState().activeCalendarSetId).toBeNull();
  });

  it("leaves the Active Calendar Set alone when a different Set is deleted", async () => {
    vi.mocked(calendarSetsApi.remove).mockResolvedValue(undefined);
    useShellStore.setState({ activeCalendarSetId: weekendSet.id });

    await useCalendarSetsStore.getState().deleteCalendarSet(workSet.id);

    expect(useShellStore.getState().activeCalendarSetId).toBe(weekendSet.id);
  });
});
