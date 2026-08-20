import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { User } from "../lib/authApi";
import type { Calendar } from "../lib/calendar";
import type { Event } from "../lib/event";
import type { Occurrence } from "../lib/occurrence";

// Same convention as the store tests: the *Api modules are mocked, the
// stores are real. What these cover is the wiring between a Save plan and
// the write it resolves to — the arms of EventModal's runSavePlan switch.
// The decision itself belongs to savePlan.test.ts and is not re-tested here.
vi.mock("../lib/eventsApi", () => ({
  eventsApi: {
    list: vi.fn(),
    create: vi.fn(),
    update: vi.fn(),
    remove: vi.fn(),
    addException: vi.fn(),
    reparentSeries: vi.fn(),
    setReminders: vi.fn(),
  },
}));
vi.mock("../lib/calendarsApi", () => ({
  calendarsApi: { list: vi.fn(), create: vi.fn() },
}));
vi.mock("../lib/attendeesApi", () => ({
  attendeesApi: {
    list: vi.fn(),
    add: vi.fn(),
    addGroup: vi.fn(),
    addEmail: vi.fn(),
    remove: vi.fn(),
    removeEmail: vi.fn(),
    setResponse: vi.fn(),
  },
}));
vi.mock("../lib/groupsApi", () => ({ groupsApi: { list: vi.fn() } }));
vi.mock("../lib/workspaceMembersApi", () => ({
  workspaceMembersApi: { list: vi.fn() },
}));
vi.mock("../lib/attachmentsApi", () => ({
  attachmentsApi: { upload: vi.fn(), remove: vi.fn(), downloadUrl: vi.fn() },
}));
vi.mock("../lib/icsApi", () => ({
  icsApi: { downloadEvent: vi.fn(), eventOversizedAttachments: vi.fn() },
}));
vi.mock("../lib/toast", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

const { eventsApi } = await import("../lib/eventsApi");
const { calendarsApi } = await import("../lib/calendarsApi");
const { attendeesApi } = await import("../lib/attendeesApi");
const { groupsApi } = await import("../lib/groupsApi");
const { workspaceMembersApi } = await import("../lib/workspaceMembersApi");
const { useAuthStore } = await import("../lib/authStore");
const { useCalendarsStore } = await import("../lib/calendarsStore");
const { useEventsStore } = await import("../lib/eventsStore");
const { useShellStore } = await import("../lib/shellStore");
const { EventModal } = await import("./EventModal");

const owned: Calendar = {
  id: "cal-1",
  name: "Personal",
  color: "#8E44ADFF",
  access: "owner",
  isOwner: true,
};

// A Calendar shared read-only: every Event field write is refused, but the
// caller's own Reminders are still theirs to set (#211, ADR-0064).
const viewerOnly: Calendar = {
  id: "cal-2",
  name: "Team",
  color: "#3498DBFF",
  access: "viewer",
  isOwner: false,
};

const user: User = {
  id: 1,
  name: "Ada",
  mustChangePassword: false,
  email: "ada@example.com",
  emailReminderChannelAvailable: false,
  invitationRepliesConfigured: false,
  syncedDeviceRemindersEnabled: false,
  weekStart: 1,
  defaultView: "week",
  timeFormat: "24h",
  workingHoursStart: null,
  workingHoursEnd: null,
};

const DAY = new Date(2026, 7, 3);

function makeEvent(overrides: Partial<Event> = {}): Event {
  return {
    id: "evt-1",
    calendarId: "cal-1",
    title: "Standup",
    start: new Date(2026, 7, 3, 9, 0),
    end: new Date(2026, 7, 3, 10, 0),
    ...overrides,
  };
}

function makeOccurrence(event: Event): Occurrence {
  return { event, start: event.start, end: event.end };
}

function seed(events: Event[], calendars: Calendar[] = [owned]) {
  useCalendarsStore.setState({ calendars });
  useShellStore.setState({
    checkedCalendarIds: new Set(calendars.map((c) => c.id)),
  });
  useEventsStore.setState({ events });
  useAuthStore.setState({
    status: "authenticated",
    user,
    accessToken: "token-123",
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(attendeesApi.list).mockResolvedValue([]);
  vi.mocked(groupsApi.list).mockResolvedValue([]);
  vi.mocked(workspaceMembersApi.list).mockResolvedValue([]);
  vi.mocked(eventsApi.create).mockResolvedValue(makeEvent());
  vi.mocked(eventsApi.update).mockResolvedValue(makeEvent());
  vi.mocked(eventsApi.setReminders).mockResolvedValue([]);
  vi.mocked(calendarsApi.create).mockResolvedValue(owned);
  seed([]);
});

function renderCreate(onClose = vi.fn()) {
  render(
    <EventModal
      mode="create"
      day={DAY}
      draft={{ start: new Date(2026, 7, 3, 9, 0), end: new Date(2026, 7, 3, 10, 0) }}
      onClose={onClose}
    />,
  );
  return onClose;
}

function renderEdit(event: Event, onClose = vi.fn()) {
  // The store's own copy has to exist: updateEvent resolves the row by id and
  // returns early when it finds nothing.
  useEventsStore.setState({ events: [event] });
  render(
    <EventModal mode="edit" occurrence={makeOccurrence(event)} onClose={onClose} />,
  );
  return onClose;
}

const saveButton = () => screen.getByRole("button", { name: "Save" });

describe("EventModal — blocked", () => {
  it("refuses to save an untitled Event", () => {
    renderCreate();
    expect(saveButton()).toBeDisabled();
  });

  it("enables Save once the form is valid", async () => {
    renderCreate();
    await userEvent.type(screen.getByLabelText("Title"), "Retro");
    expect(saveButton()).toBeEnabled();
  });
});

describe("EventModal — create", () => {
  it("creates the Event and closes", async () => {
    const onClose = renderCreate();

    await userEvent.type(screen.getByLabelText("Title"), "Retro");
    await userEvent.click(saveButton());

    expect(eventsApi.create).toHaveBeenCalledWith(
      "token-123",
      expect.objectContaining({ title: "Retro", calendarId: "cal-1" }),
    );
    expect(onClose).toHaveBeenCalled();
  });
});

describe("EventModal — creating a calendar from the empty state (#233)", () => {
  it("explains why Save is disabled while no Calendar is selected", async () => {
    seed([], []);
    renderCreate();

    expect(
      screen.getByText("You don't have any calendars yet."),
    ).toBeInTheDocument();
    await userEvent.type(screen.getByLabelText("Title"), "Retro");

    expect(saveButton()).toBeDisabled();
    // Matches the empty-state copy already on screen, rather than naming an
    // action ("select a calendar") with no picker present to perform it.
    expect(saveButton()).toHaveAttribute(
      "title",
      "You don't have any calendars yet.",
    );
  });

  it("adopts the newly created Calendar and enables Save", async () => {
    seed([], []);
    renderCreate();

    await userEvent.type(screen.getByLabelText("Title"), "Retro");
    await userEvent.click(
      screen.getByRole("button", { name: "Create a calendar" }),
    );

    const createDialog = within(
      screen.getByRole("dialog", { name: "New calendar" }),
    );
    await userEvent.type(createDialog.getByLabelText("Name"), "Personal");
    await userEvent.click(createDialog.getByRole("button", { name: "Save" }));

    expect(
      screen.queryByRole("dialog", { name: "New calendar" }),
    ).not.toBeInTheDocument();

    const picker = screen.getByRole("combobox", { name: "Calendar" });
    expect(picker).toHaveTextContent("Personal");
    expect(saveButton()).toBeEnabled();

    await userEvent.click(saveButton());

    expect(eventsApi.create).toHaveBeenCalledWith(
      "token-123",
      expect.objectContaining({ title: "Retro" }),
    );
    const created = vi.mocked(eventsApi.create).mock.calls[0]?.[1];
    expect(created?.calendarId).not.toBe("");
  });

  it("un-selects the Calendar again if its optimistic create is rejected by the server", async () => {
    vi.mocked(calendarsApi.create).mockRejectedValueOnce(new Error("boom"));
    seed([], []);
    renderCreate();

    await userEvent.type(screen.getByLabelText("Title"), "Retro");
    await userEvent.click(
      screen.getByRole("button", { name: "Create a calendar" }),
    );

    const createDialog = within(
      screen.getByRole("dialog", { name: "New calendar" }),
    );
    await userEvent.type(createDialog.getByLabelText("Name"), "Personal");
    await userEvent.click(createDialog.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(
        screen.getByText("You don't have any calendars yet."),
      ).toBeInTheDocument();
    });
    expect(saveButton()).toBeDisabled();
    expect(saveButton()).toHaveAttribute(
      "title",
      "You don't have any calendars yet.",
    );

    await userEvent.click(saveButton());
    expect(eventsApi.create).not.toHaveBeenCalled();
  });

  it("cancelling the New calendar dialog leaves the empty state in place", async () => {
    seed([], []);
    renderCreate();

    await userEvent.click(
      screen.getByRole("button", { name: "Create a calendar" }),
    );
    const createDialog = within(
      screen.getByRole("dialog", { name: "New calendar" }),
    );
    await userEvent.click(
      createDialog.getByRole("button", { name: "Cancel" }),
    );

    expect(
      screen.queryByRole("dialog", { name: "New calendar" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByText("You don't have any calendars yet."),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Create a calendar" }),
    ).toBeInTheDocument();
  });
});

describe("EventModal — editing a non-recurring Event", () => {
  it("updates the Event without asking for a scope", async () => {
    const event = makeEvent();
    const onClose = renderEdit(event);

    const title = screen.getByLabelText("Title");
    await userEvent.clear(title);
    await userEvent.type(title, "Renamed");
    await userEvent.click(saveButton());

    expect(eventsApi.update).toHaveBeenCalledWith(
      "token-123",
      "evt-1",
      expect.objectContaining({ title: "Renamed" }),
    );
    expect(
      screen.queryByText("Edit recurring event"),
    ).not.toBeInTheDocument();
    expect(onClose).toHaveBeenCalled();
  });
});

describe("EventModal — editing a recurring Occurrence", () => {
  const recurring = () => makeEvent({ rrule: "FREQ=WEEKLY;BYDAY=MO" });

  it("asks which Occurrences a change applies to, then applies the chosen scope", async () => {
    const onClose = renderEdit(recurring());

    const title = screen.getByLabelText("Title");
    await userEvent.clear(title);
    await userEvent.type(title, "Renamed");
    await userEvent.click(saveButton());

    // The write is held until a scope is chosen.
    expect(eventsApi.update).not.toHaveBeenCalled();
    expect(screen.getByText("Edit recurring event")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("radio", { name: "All events" }));
    await userEvent.click(screen.getByRole("button", { name: "Edit" }));

    expect(eventsApi.update).toHaveBeenCalledWith(
      "token-123",
      "evt-1",
      expect.objectContaining({ title: "Renamed" }),
    );
    expect(onClose).toHaveBeenCalled();
  });

  it("writes nothing when the scope picker is cancelled", async () => {
    const onClose = renderEdit(recurring());

    const title = screen.getByLabelText("Title");
    await userEvent.clear(title);
    await userEvent.type(title, "Renamed");
    await userEvent.click(saveButton());
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));

    expect(eventsApi.update).not.toHaveBeenCalled();
    expect(eventsApi.create).not.toHaveBeenCalled();
    expect(onClose).not.toHaveBeenCalled();
  });

  it("closes on a no-op save rather than prompting for a scope (#141)", async () => {
    const onClose = renderEdit(recurring());

    await userEvent.click(saveButton());

    expect(screen.queryByText("Edit recurring event")).not.toBeInTheDocument();
    expect(eventsApi.update).not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalled();
  });
});

describe("EventModal — an Event crossing midnight (#231)", () => {
  it("defaults a drag-created draft's End date to the same day as its Start", () => {
    renderCreate();

    expect(screen.getByLabelText("Start date")).toHaveValue("2026-08-03");
    expect(screen.getByLabelText("End date")).toHaveValue("2026-08-03");
  });

  it("saves an Event whose End date is later than its Start date", async () => {
    const onClose = renderCreate();

    await userEvent.type(screen.getByLabelText("Title"), "Night shift");
    fireEvent.change(screen.getByLabelText("Start time"), {
      target: { value: "23:00" },
    });
    fireEvent.change(screen.getByLabelText("End time"), {
      target: { value: "01:00" },
    });
    fireEvent.change(screen.getByLabelText("End date"), {
      target: { value: "2026-08-04" },
    });
    expect(saveButton()).toBeEnabled();
    await userEvent.click(saveButton());

    expect(eventsApi.create).toHaveBeenCalledWith(
      "token-123",
      expect.objectContaining({
        title: "Night shift",
        start: new Date(2026, 7, 3, 23, 0),
        end: new Date(2026, 7, 4, 1, 0),
      }),
    );
    expect(onClose).toHaveBeenCalled();
  });

  it("still refuses an end time at-or-before start time on the same date", () => {
    renderCreate();

    fireEvent.change(screen.getByLabelText("Start time"), {
      target: { value: "10:00" },
    });
    fireEvent.change(screen.getByLabelText("End time"), {
      target: { value: "09:00" },
    });

    expect(saveButton()).toBeDisabled();
    expect(
      screen.getByText("End time must be after start time."),
    ).toBeInTheDocument();
  });

  it("refuses an End date earlier than the Start date, naming the date", () => {
    renderCreate();

    fireEvent.change(screen.getByLabelText("End date"), {
      target: { value: "2026-08-02" },
    });

    expect(saveButton()).toBeDisabled();
    expect(
      screen.getByText("End date must be on or after the start date."),
    ).toBeInTheDocument();
    expect(
      screen.queryByText("End time must be after start time."),
    ).not.toBeInTheDocument();
  });

  it("keeps the date range in state when All day is toggled off again", async () => {
    renderCreate();

    fireEvent.change(screen.getByLabelText("End date"), {
      target: { value: "2026-08-05" },
    });
    await userEvent.click(screen.getByRole("checkbox"));
    expect(screen.queryByLabelText("End date")).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole("checkbox"));
    expect(screen.getByLabelText("End date")).toHaveValue("2026-08-05");
  });

  it("shows both dates on an existing multi-day Event, and editing the End date round-trips", async () => {
    const event = makeEvent({
      start: new Date(2026, 7, 3, 23, 0),
      end: new Date(2026, 7, 4, 1, 0),
    });
    const onClose = renderEdit(event);

    expect(screen.getByLabelText("Start date")).toHaveValue("2026-08-03");
    expect(screen.getByLabelText("End date")).toHaveValue("2026-08-04");

    fireEvent.change(screen.getByLabelText("End date"), {
      target: { value: "2026-08-06" },
    });
    await userEvent.click(saveButton());

    expect(eventsApi.update).toHaveBeenCalledWith(
      "token-123",
      "evt-1",
      expect.objectContaining({
        start: new Date(2026, 7, 3, 23, 0),
        end: new Date(2026, 7, 6, 1, 0),
      }),
    );
    expect(onClose).toHaveBeenCalled();
  });
});

describe("EventModal — a read-only Event", () => {
  // A Viewer's own Reminders are the one write the Event still accepts
  // (#211, ADR-0064) — never an Event-field write, and never a series op.
  const shared = () => makeEvent({ id: "evt-9", calendarId: "cal-2" });

  function renderShared() {
    seed([shared()], [owned, viewerOnly]);
    return renderEdit(shared());
  }

  it("disables Save while the Reminders draft is untouched", async () => {
    renderShared();
    expect(saveButton()).toBeDisabled();
  });

  it("writes only the Reminders once one is added", async () => {
    const onClose = renderShared();

    await userEvent.click(screen.getByRole("button", { name: "More options" }));
    await userEvent.click(screen.getByRole("button", { name: "Add reminder" }));

    expect(saveButton()).toBeEnabled();
    await userEvent.click(saveButton());

    expect(eventsApi.setReminders).toHaveBeenCalledWith("token-123", "evt-9", [
      { offsetMinutes: 10, channel: "notification" },
    ]);
    expect(eventsApi.update).not.toHaveBeenCalled();
    expect(eventsApi.create).not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalled();
  });
});
