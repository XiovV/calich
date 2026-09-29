import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

// Same convention as the other component tests: the *Api modules are
// mocked, the stores are real.
vi.mock("../../lib/calendarsApi", () => ({
  calendarsApi: {
    create: vi.fn(async () => undefined),
    getDefaultReminders: vi.fn(async () => ({ timed: [], allDay: [] })),
  },
}));
vi.mock("../../lib/calendarSetsApi", async () => {
  const actual = await vi.importActual<typeof import("../../lib/calendarSetsApi")>(
    "../../lib/calendarSetsApi",
  );
  return {
    ...actual,
    calendarSetsApi: { ...actual.calendarSetsApi, addCalendar: vi.fn(async () => undefined) },
  };
});
vi.mock("../../lib/toast", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));

const { useAuthStore } = await import("../../lib/authStore");
const { useCalendarsStore } = await import("../../lib/calendarsStore");
const { useShellStore } = await import("../../lib/shellStore");
const { useCalendarSetsStore } = await import("../../lib/calendarSetsStore");
const { calendarSetsApi } = await import("../../lib/calendarSetsApi");
const { CalendarModal } = await import("./CalendarModal");

const workSet = { id: 1, name: "Work", calendarIds: [] };

beforeEach(() => {
  vi.clearAllMocks();
  useAuthStore.setState({ accessToken: "token-123" });
  useCalendarsStore.setState({ calendars: [] });
  useShellStore.setState({ checkedCalendarIds: new Set<string>(), activeCalendarSetId: null });
  useCalendarSetsStore.setState({ calendarSets: [] });
});

describe("CalendarModal add-to-set checkbox (#308)", () => {
  it("is absent when on All calendars", () => {
    render(<CalendarModal mode="create" onClose={vi.fn()} />);
    expect(screen.queryByRole("checkbox", { name: /Add to/ })).not.toBeInTheDocument();
  });

  it("is absent when editing an existing calendar, even with a Set active", () => {
    useCalendarSetsStore.setState({ calendarSets: [workSet] });
    useShellStore.setState({ activeCalendarSetId: workSet.id });
    render(
      <CalendarModal
        mode="edit"
        calendar={{ id: "cal-1", name: "Personal", color: "#8E44ADFF", access: "owner", isOwner: true }}
        onClose={vi.fn()}
      />,
    );
    expect(screen.queryByRole("checkbox", { name: /Add to/ })).not.toBeInTheDocument();
  });

  it("names the Active Calendar Set and starts unticked", () => {
    useCalendarSetsStore.setState({ calendarSets: [workSet] });
    useShellStore.setState({ activeCalendarSetId: workSet.id });
    render(<CalendarModal mode="create" onClose={vi.fn()} />);

    const checkbox = screen.getByRole("checkbox", { name: /Add to Work/ });
    expect(checkbox).not.toBeChecked();
  });

  it("adds the new calendar to the Active Calendar Set when ticked", async () => {
    useCalendarSetsStore.setState({ calendarSets: [workSet] });
    useShellStore.setState({ activeCalendarSetId: workSet.id });
    render(<CalendarModal mode="create" onClose={vi.fn()} />);

    await userEvent.type(screen.getByLabelText("Name"), "New calendar");
    await userEvent.click(screen.getByRole("checkbox", { name: /Add to Work/ }));
    await userEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(calendarSetsApi.addCalendar).toHaveBeenCalledWith(
      "token-123",
      workSet.id,
      expect.any(String),
    );
  });

  it("does not add the new calendar to the Set when left unticked", async () => {
    useCalendarSetsStore.setState({ calendarSets: [workSet] });
    useShellStore.setState({ activeCalendarSetId: workSet.id });
    render(<CalendarModal mode="create" onClose={vi.fn()} />);

    await userEvent.type(screen.getByLabelText("Name"), "New calendar");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(calendarSetsApi.addCalendar).not.toHaveBeenCalled();
  });
});
