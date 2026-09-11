import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { SubscriptionPreview } from "../../lib/calendarsApi";

// Same convention as the other component tests: the *Api modules are
// mocked, the stores are real.
vi.mock("../../lib/calendarsApi", () => ({
  calendarsApi: {
    previewSubscription: vi.fn(),
    subscribe: vi.fn(),
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
const { calendarsApi } = await import("../../lib/calendarsApi");
const { calendarSetsApi } = await import("../../lib/calendarSetsApi");
const { SubscribeCalendarModal } = await import("./SubscribeCalendarModal");

const workSet = { id: 1, name: "Work", calendarIds: [] };

const preview: SubscriptionPreview = {
  name: "Holidays",
  color: "#8E44ADFF",
  eventCount: 3,
  rangeStart: null,
  rangeEnd: null,
};

beforeEach(() => {
  vi.clearAllMocks();
  useAuthStore.setState({ accessToken: "token-123" });
  useCalendarsStore.setState({ calendars: [] });
  useShellStore.setState({ checkedCalendarIds: new Set<string>(), activeCalendarSetId: null });
  useCalendarSetsStore.setState({ calendarSets: [] });
});

async function previewFeed() {
  await userEvent.type(
    screen.getByLabelText("Calendar URL"),
    "https://example.com/calendar.ics",
  );
  await userEvent.click(screen.getByRole("button", { name: "Preview" }));
}

describe("SubscribeCalendarModal add-to-set checkbox (#308)", () => {
  it("is absent when on All calendars", async () => {
    vi.mocked(calendarsApi.previewSubscription).mockResolvedValue(preview);
    render(<SubscribeCalendarModal onClose={vi.fn()} />);
    await previewFeed();
    await screen.findByLabelText("Name");

    expect(screen.queryByRole("checkbox", { name: /Add to/ })).not.toBeInTheDocument();
  });

  it("names the Active Calendar Set and starts unticked", async () => {
    useCalendarSetsStore.setState({ calendarSets: [workSet] });
    useShellStore.setState({ activeCalendarSetId: workSet.id });
    vi.mocked(calendarsApi.previewSubscription).mockResolvedValue(preview);
    render(<SubscribeCalendarModal onClose={vi.fn()} />);
    await previewFeed();

    const checkbox = await screen.findByRole("checkbox", { name: /Add to Work/ });
    expect(checkbox).not.toBeChecked();
  });

  it("adds the subscribed calendar to the Active Calendar Set when ticked", async () => {
    useCalendarSetsStore.setState({ calendarSets: [workSet] });
    useShellStore.setState({ activeCalendarSetId: workSet.id });
    vi.mocked(calendarsApi.previewSubscription).mockResolvedValue(preview);
    vi.mocked(calendarsApi.subscribe).mockResolvedValue({
      id: "cal-1",
      name: "Holidays",
      color: "#8E44ADFF",
      access: "owner",
      isOwner: true,
    });
    render(<SubscribeCalendarModal onClose={vi.fn()} />);
    await previewFeed();

    await userEvent.click(await screen.findByRole("checkbox", { name: /Add to Work/ }));
    await userEvent.click(screen.getByRole("button", { name: "Subscribe" }));

    expect(calendarSetsApi.addCalendar).toHaveBeenCalledWith("token-123", workSet.id, "cal-1");
  });

  it("does not add the subscribed calendar to the Set when left unticked", async () => {
    useCalendarSetsStore.setState({ calendarSets: [workSet] });
    useShellStore.setState({ activeCalendarSetId: workSet.id });
    vi.mocked(calendarsApi.previewSubscription).mockResolvedValue(preview);
    vi.mocked(calendarsApi.subscribe).mockResolvedValue({
      id: "cal-1",
      name: "Holidays",
      color: "#8E44ADFF",
      access: "owner",
      isOwner: true,
    });
    render(<SubscribeCalendarModal onClose={vi.fn()} />);
    await previewFeed();
    await screen.findByRole("checkbox", { name: /Add to Work/ });

    await userEvent.click(screen.getByRole("button", { name: "Subscribe" }));

    expect(calendarSetsApi.addCalendar).not.toHaveBeenCalled();
  });
});
