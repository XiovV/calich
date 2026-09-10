import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

// Same convention as the other component tests: the *Api modules are
// mocked, the stores are real.
vi.mock("../lib/connectionsApi", async () => {
  const actual = await vi.importActual<typeof import("../lib/connectionsApi")>("../lib/connectionsApi");
  return {
    ...actual,
    connectionsApi: {
      listPickerCalendars: vi.fn(),
      importCalendars: vi.fn(),
    },
  };
});
vi.mock("../lib/calendarsApi", () => ({ calendarsApi: { list: vi.fn(), remove: vi.fn() } }));
vi.mock("../lib/eventsApi", () => ({ eventsApi: { list: vi.fn() } }));

const { connectionsApi } = await import("../lib/connectionsApi");
const { calendarsApi } = await import("../lib/calendarsApi");
const { eventsApi } = await import("../lib/eventsApi");
const { useAuthStore } = await import("../lib/authStore");
const { useCalendarsStore } = await import("../lib/calendarsStore");
const { useEventsStore } = await import("../lib/eventsStore");
const { useShellStore } = await import("../lib/shellStore");
const { useWorkspacesStore } = await import("../lib/workspacesStore");
const { CalendarPickerModal } = await import("./CalendarPickerModal");

const primary = {
  id: "primary",
  name: "someone@gmail.com",
  color: "#0B8043FF",
  selected: true,
  writable: true,
  importedHere: false,
  importedElsewhere: false,
  shareCount: 0,
};
const sharedIn = {
  id: "shared-in@group.calendar.google.com",
  name: "Team calendar",
  color: "#7627BBFF",
  selected: false,
  writable: false,
  importedHere: false,
  importedElsewhere: false,
  shareCount: 0,
};

beforeEach(() => {
  vi.clearAllMocks();
  useAuthStore.setState({ accessToken: "token-123" });
  useCalendarsStore.setState({ calendars: [] });
  useEventsStore.setState({ events: [] });
  useWorkspacesStore.setState({ activeWorkspaceId: 7 });
  vi.mocked(calendarsApi.list).mockResolvedValue([]);
  vi.mocked(eventsApi.list).mockResolvedValue([]);
});

afterEach(() => {
  useWorkspacesStore.setState({ activeWorkspaceId: null });
});

describe("CalendarPickerModal", () => {
  it("pre-checks rows Google itself has selected, and marks unwritable rows read-only", async () => {
    vi.mocked(connectionsApi.listPickerCalendars).mockResolvedValue([primary, sharedIn]);
    render(<CalendarPickerModal connectionId={1} onClose={vi.fn()} />);

    expect(await screen.findByLabelText("someone@gmail.com")).toBeChecked();
    expect(screen.getByLabelText("Team calendar")).not.toBeChecked();
    expect(screen.getByText("Read-only")).toBeInTheDocument();
  });

  it("imports exactly the checked rows and refreshes calendars on confirm", async () => {
    vi.mocked(connectionsApi.listPickerCalendars).mockResolvedValue([primary, sharedIn]);
    vi.mocked(connectionsApi.importCalendars).mockResolvedValue([
      { id: "cal-1", name: "someone@gmail.com", color: "#0B8043FF" },
    ]);
    const onClose = vi.fn();
    render(<CalendarPickerModal connectionId={1} onClose={onClose} />);

    await screen.findByLabelText("someone@gmail.com");
    await userEvent.click(screen.getByRole("button", { name: /Add 1 calendar/ }));

    await waitFor(() => expect(connectionsApi.importCalendars).toHaveBeenCalledWith("token-123", 1, ["primary"]));
    expect(calendarsApi.list).toHaveBeenCalled();
    expect(onClose).toHaveBeenCalled();
  });

  // The import created a new Linked Calendar server-side; fetchCalendars is
  // the only way this session learns its id. Without a reconcile after it —
  // the same gap ImportExportSection's ICS import closed for #229 — the new
  // Calendar's toggle stayed unset until a reload.
  it("checks the imported calendar's toggle immediately, without a reload", async () => {
    vi.mocked(connectionsApi.listPickerCalendars).mockResolvedValue([primary]);
    vi.mocked(connectionsApi.importCalendars).mockResolvedValue([
      { id: "cal-1", name: "someone@gmail.com", color: "#0B8043FF" },
    ]);
    vi.mocked(calendarsApi.list).mockResolvedValue([
      { id: "cal-1", name: "someone@gmail.com", color: "#0B8043FF", isOwner: true, access: "owner" },
    ]);
    render(<CalendarPickerModal connectionId={1} onClose={vi.fn()} />);

    await screen.findByLabelText("someone@gmail.com");
    await userEvent.click(screen.getByRole("button", { name: /Add 1 calendar/ }));

    await waitFor(() =>
      expect(useShellStore.getState().checkedCalendarIds.has("cal-1")).toBe(true),
    );
    expect(useShellStore.getState().knownCalendarIds.has("cal-1")).toBe(true);
  });

  // The import created a new Linked Calendar's Events server-side too;
  // nothing refetches eventsStore after Confirm, so its Events sat missing
  // from the grid until the next mount/focus/workspace-change refetch —
  // visible as "alt-tab and back makes them appear".
  it("shows the imported calendar's events immediately, without a reload", async () => {
    vi.mocked(connectionsApi.listPickerCalendars).mockResolvedValue([primary]);
    vi.mocked(connectionsApi.importCalendars).mockResolvedValue([
      { id: "cal-1", name: "someone@gmail.com", color: "#0B8043FF" },
    ]);
    vi.mocked(calendarsApi.list).mockResolvedValue([
      { id: "cal-1", name: "someone@gmail.com", color: "#0B8043FF", isOwner: true, access: "owner" },
    ]);
    vi.mocked(eventsApi.list).mockResolvedValue([
      {
        id: "evt-1",
        calendarId: "cal-1",
        title: "Google calendar event",
        start: new Date("2026-09-08T11:15:00Z"),
        end: new Date("2026-09-08T14:15:00Z"),
      },
    ]);
    render(<CalendarPickerModal connectionId={1} onClose={vi.fn()} />);

    await screen.findByLabelText("someone@gmail.com");
    await userEvent.click(screen.getByRole("button", { name: /Add 1 calendar/ }));

    await waitFor(() =>
      expect(useEventsStore.getState().events.some((event) => event.id === "evt-1")).toBe(
        true,
      ),
    );
  });

  it("toggling a row changes what gets imported", async () => {
    vi.mocked(connectionsApi.listPickerCalendars).mockResolvedValue([primary, sharedIn]);
    vi.mocked(connectionsApi.importCalendars).mockResolvedValue([]);
    render(<CalendarPickerModal connectionId={1} onClose={vi.fn()} />);

    await userEvent.click(await screen.findByLabelText("Team calendar"));
    await userEvent.click(screen.getByRole("button", { name: /Add 2 calendars/ }));

    await waitFor(() =>
      expect(connectionsApi.importCalendars).toHaveBeenCalledWith(
        "token-123",
        1,
        expect.arrayContaining(["primary", "shared-in@group.calendar.google.com"]),
      ),
    );
  });

  it("shows an inline error and does not close when import fails", async () => {
    vi.mocked(connectionsApi.listPickerCalendars).mockResolvedValue([primary]);
    vi.mocked(connectionsApi.importCalendars).mockRejectedValue(new Error("network error"));
    const onClose = vi.fn();
    render(<CalendarPickerModal connectionId={1} onClose={onClose} />);

    await screen.findByLabelText("someone@gmail.com");
    await userEvent.click(screen.getByRole("button", { name: /Add 1 calendar/ }));

    expect(await screen.findByRole("alert")).toHaveTextContent("network error");
    expect(onClose).not.toHaveBeenCalled();
  });

  // The picker's own trigger — Connect's redirect landing on a fresh
  // full-page load — is exactly the moment AppShell's own fetchWorkspaces()
  // may not have resolved yet. Firing the picker's fetch before that lands
  // throws "No active workspace" on every single Connect, since the two
  // races every time (#286).
  it("waits for the active workspace to resolve before fetching", async () => {
    useWorkspacesStore.setState({ activeWorkspaceId: null });
    vi.mocked(connectionsApi.listPickerCalendars).mockResolvedValue([primary]);
    render(<CalendarPickerModal connectionId={1} onClose={vi.fn()} />);

    expect(screen.getByText("Loading calendars…")).toBeInTheDocument();
    expect(connectionsApi.listPickerCalendars).not.toHaveBeenCalled();

    useWorkspacesStore.setState({ activeWorkspaceId: 7 });

    expect(await screen.findByLabelText("someone@gmail.com")).toBeInTheDocument();
    expect(connectionsApi.listPickerCalendars).toHaveBeenCalledWith("token-123", 1);
  });

  it("shows an empty state when the account has no calendars", async () => {
    vi.mocked(connectionsApi.listPickerCalendars).mockResolvedValue([]);
    render(<CalendarPickerModal connectionId={1} onClose={vi.fn()} />);

    expect(await screen.findByText("This account has no calendars to bring in.")).toBeInTheDocument();
  });

  // #295: the re-run shows this Workspace's state — a row imported here is
  // checked, a row imported into another Workspace is noted and unchecked.
  it("checks rows imported here and notes rows imported into another workspace", async () => {
    vi.mocked(connectionsApi.listPickerCalendars).mockResolvedValue([
      { ...primary, selected: false, importedHere: true, localCalendarId: "cal-1" },
      { ...sharedIn, importedElsewhere: true },
    ]);
    render(<CalendarPickerModal connectionId={1} onClose={vi.fn()} />);

    expect(await screen.findByLabelText("someone@gmail.com")).toBeChecked();
    expect(screen.getByLabelText("Team calendar")).not.toBeChecked();
    expect(screen.getByText("Already in another workspace")).toBeInTheDocument();
    // Nothing new to import, so Confirm just closes.
    expect(screen.getByRole("button", { name: "Done" })).toBeInTheDocument();
  });

  // #295: unchecking a previously-imported row deletes the Linked Calendar,
  // behind a confirmation that names the Shares it carries.
  it("deletes an imported calendar when it is unchecked and the delete is confirmed", async () => {
    const { calendarsApi } = await import("../lib/calendarsApi");
    vi.mocked(calendarsApi.remove).mockResolvedValue(undefined);
    vi.mocked(connectionsApi.listPickerCalendars).mockResolvedValue([
      { ...primary, selected: false, importedHere: true, localCalendarId: "cal-1", shareCount: 2 },
    ]);
    render(<CalendarPickerModal connectionId={1} onClose={vi.fn()} />);

    await userEvent.click(await screen.findByLabelText("someone@gmail.com"));

    expect(await screen.findByText('Remove “someone@gmail.com”?')).toBeInTheDocument();
    expect(screen.getByText(/shared with 2 people/)).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Remove" }));

    await waitFor(() => expect(calendarsApi.remove).toHaveBeenCalledWith("token-123", "cal-1"));
    await waitFor(() => expect(screen.getByLabelText("someone@gmail.com")).not.toBeChecked());
  });

  it("keeps the row imported and surfaces an error when the delete fails", async () => {
    const { calendarsApi } = await import("../lib/calendarsApi");
    vi.mocked(calendarsApi.remove).mockRejectedValue(new Error("boom"));
    vi.mocked(connectionsApi.listPickerCalendars).mockResolvedValue([
      { ...primary, selected: false, importedHere: true, localCalendarId: "cal-1" },
    ]);
    render(<CalendarPickerModal connectionId={1} onClose={vi.fn()} />);

    await userEvent.click(await screen.findByLabelText("someone@gmail.com"));
    await userEvent.click(await screen.findByRole("button", { name: "Remove" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(/Couldn't remove/);
    expect(screen.getByLabelText("someone@gmail.com")).toBeChecked();
  });

  it("keeps an imported calendar when the delete confirmation is cancelled", async () => {
    const { calendarsApi } = await import("../lib/calendarsApi");
    vi.mocked(connectionsApi.listPickerCalendars).mockResolvedValue([
      { ...primary, selected: false, importedHere: true, localCalendarId: "cal-1" },
    ]);
    render(<CalendarPickerModal connectionId={1} onClose={vi.fn()} />);

    await userEvent.click(await screen.findByLabelText("someone@gmail.com"));
    const dialog = await screen.findByRole("alertdialog");
    await userEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));

    expect(calendarsApi.remove).not.toHaveBeenCalled();
    expect(screen.getByLabelText("someone@gmail.com")).toBeChecked();
  });
});
