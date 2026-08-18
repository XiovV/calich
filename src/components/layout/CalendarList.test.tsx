import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Calendar } from "../../lib/calendar";

// Same convention as the other component tests: the *Api modules are mocked,
// the stores are real.
vi.mock("../../lib/calendarsApi", () => ({ calendarsApi: { list: vi.fn() } }));
vi.mock("../../lib/icsApi", () => ({
  icsApi: { downloadCalendar: vi.fn(), calendarOversizedAttachments: vi.fn() },
}));
vi.mock("../../lib/toast", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));

const { useAuthStore } = await import("../../lib/authStore");
const { useCalendarsStore } = await import("../../lib/calendarsStore");
const { useEventsStore } = await import("../../lib/eventsStore");
const { useShellStore } = await import("../../lib/shellStore");
const { CalendarList } = await import("./CalendarList");

const owned: Calendar = {
  id: "cal-1",
  name: "Personal",
  color: "#8E44ADFF",
  access: "owner",
  isOwner: true,
};

// A Calendar the viewer has Editor Access to: they can write its Events,
// but sharing stays the Owner's alone (ADR-0034).
const editorAccess: Calendar = {
  id: "cal-2",
  name: "Team",
  color: "#3498DBFF",
  access: "editor",
  isOwner: false,
  ownerName: "Bob",
};

beforeEach(() => {
  vi.clearAllMocks();
  useAuthStore.setState({ accessToken: "token-123" });
  useEventsStore.setState({ events: [] });
  useShellStore.setState({ checkedCalendarIds: new Set<string>() });
});

async function openMenu(calendarName: string) {
  await userEvent.click(screen.getByRole("button", { name: `${calendarName} actions` }));
}

describe("CalendarList row menu", () => {
  it("offers Share on a calendar the viewer owns", async () => {
    useCalendarsStore.setState({ calendars: [owned] });
    render(<CalendarList />);

    await openMenu("Personal");

    expect(await screen.findByRole("menuitem", { name: "Share" })).toBeInTheDocument();
  });

  it("omits Share entirely on a calendar the viewer only has Editor Access to", async () => {
    useCalendarsStore.setState({ calendars: [editorAccess] });
    render(<CalendarList />);

    await openMenu("Team");

    expect(await screen.findByRole("menuitem", { name: "Edit" })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "Leave" })).toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: "Share" })).not.toBeInTheDocument();
  });
});
