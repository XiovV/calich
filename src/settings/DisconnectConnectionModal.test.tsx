import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

// Same convention as CalendarPickerModal.test.tsx: the *Api modules are
// mocked, the stores are real.
vi.mock("../lib/connectionsApi", async () => {
  const actual =
    await vi.importActual<typeof import("../lib/connectionsApi")>("../lib/connectionsApi");
  return {
    ...actual,
    connectionsApi: {
      disconnectImpact: vi.fn(),
      disconnect: vi.fn(),
    },
  };
});
vi.mock("../lib/calendarsApi", () => ({ calendarsApi: { list: vi.fn() } }));

const { connectionsApi } = await import("../lib/connectionsApi");
const { calendarsApi } = await import("../lib/calendarsApi");
const { useAuthStore } = await import("../lib/authStore");
const { useCalendarsStore } = await import("../lib/calendarsStore");
const { useEventsStore } = await import("../lib/eventsStore");
const { useShellStore } = await import("../lib/shellStore");
const { DisconnectConnectionModal } = await import("./DisconnectConnectionModal");

const linkedCalendar = { id: "cal-1", name: "someone@gmail.com", shareCount: 0 };

const orphanedEvent = {
  id: "evt-1",
  calendarId: "cal-1",
  title: "Google calendar event",
  start: new Date("2026-09-08T11:15:00Z"),
  end: new Date("2026-09-08T14:15:00Z"),
};

beforeEach(() => {
  vi.clearAllMocks();
  useAuthStore.setState({ accessToken: "token-123" });
  useCalendarsStore.setState({ calendars: [] });
  useEventsStore.setState({ events: [orphanedEvent] });
  useShellStore.setState({ checkedCalendarIds: new Set(["cal-1"]) });
  vi.mocked(calendarsApi.list).mockResolvedValue([]);
  vi.mocked(connectionsApi.disconnectImpact).mockResolvedValue({
    linkedCalendars: [linkedCalendar],
  });
  vi.mocked(connectionsApi.disconnect).mockResolvedValue(undefined);
});

afterEach(() => {
  useShellStore.setState({ checkedCalendarIds: new Set() });
});

describe("DisconnectConnectionModal", () => {
  // Disconnecting with the "delete" disposition removes the Linked
  // Calendars and their Events server-side, but nothing previously pruned
  // the client-side eventsStore — an already-loaded Event for a deleted
  // Calendar stayed in the grid, rendered gray (its Calendar no longer
  // resolves), until a full page reload re-fetched Events from scratch.
  it("removes the deleted calendar's events immediately, without a reload", async () => {
    render(
      <DisconnectConnectionModal
        connectionId={1}
        accountEmail="someone@gmail.com"
        onClose={vi.fn()}
      />,
    );

    await screen.findByText(/1 calendar came in from this account/);
    await userEvent.click(screen.getByRole("radio", { name: /Delete the calendars/ }));
    await userEvent.click(screen.getByRole("button", { name: "Disconnect and delete" }));

    await waitFor(() => expect(connectionsApi.disconnect).toHaveBeenCalledWith("token-123", 1, "delete"));
    expect(
      useEventsStore.getState().events.some((event) => event.calendarId === "cal-1"),
    ).toBe(false);
    expect(useShellStore.getState().checkedCalendarIds.has("cal-1")).toBe(false);
  });
});
