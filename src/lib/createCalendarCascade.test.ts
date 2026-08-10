import { beforeEach, expect, it, vi } from "vitest";

vi.mock("./calendarsApi", () => ({
  calendarsApi: {
    create: vi.fn(),
  },
}));

vi.mock("./toast", () => ({
  toast: { error: vi.fn() },
}));

const { calendarsApi } = await import("./calendarsApi");
const { useAuthStore } = await import("./authStore");
const { useCalendarsStore } = await import("./calendarsStore");
const { useShellStore } = await import("./shellStore");
const { createCalendarCascade } = await import("./createCalendarCascade");

const personal = {
  id: "cal-1",
  name: "Personal",
  color: "peacock" as const,
  isOwner: true,
  access: "owner" as const,
};

beforeEach(() => {
  vi.clearAllMocks();
  useAuthStore.setState({ accessToken: "token-123" });
  useCalendarsStore.setState({ calendars: [] });
  useShellStore.setState({
    checkedCalendarIds: new Set(),
    knownCalendarIds: new Set(),
  });
});

it("checks and marks the calendar known immediately, before the API call resolves", () => {
  let resolveCreate: () => void = () => {};
  vi.mocked(calendarsApi.create).mockReturnValue(
    new Promise((resolve) => {
      resolveCreate = () => resolve(personal);
    }),
  );

  const promise = createCalendarCascade(personal);

  expect(useShellStore.getState().checkedCalendarIds.has("cal-1")).toBe(true);
  expect(useShellStore.getState().knownCalendarIds.has("cal-1")).toBe(true);

  resolveCreate();
  return promise;
});

it("leaves the calendar checked and known once the create succeeds", async () => {
  vi.mocked(calendarsApi.create).mockResolvedValue(personal);

  await createCalendarCascade(personal);

  expect(useShellStore.getState().checkedCalendarIds.has("cal-1")).toBe(true);
  expect(useShellStore.getState().knownCalendarIds.has("cal-1")).toBe(true);
});

it("rolls back the checked id, but not the known id, when the create fails", async () => {
  vi.mocked(calendarsApi.create).mockRejectedValue(new Error("network error"));

  await createCalendarCascade(personal);

  expect(useCalendarsStore.getState().calendars).toEqual([]);
  expect(useShellStore.getState().checkedCalendarIds.has("cal-1")).toBe(false);
});

it("survives a later reconcile without re-checking a calendar the caller deliberately unchecked", async () => {
  vi.mocked(calendarsApi.create).mockResolvedValue(personal);

  await createCalendarCascade(personal);
  useShellStore.getState().removeCheckedCalendarId("cal-1");

  useShellStore.getState().reconcileCheckedCalendarIds(["cal-1"]);

  expect(useShellStore.getState().checkedCalendarIds.has("cal-1")).toBe(false);
});
