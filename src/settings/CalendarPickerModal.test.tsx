import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
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
vi.mock("../lib/calendarsApi", () => ({ calendarsApi: { list: vi.fn() } }));

const { connectionsApi } = await import("../lib/connectionsApi");
const { calendarsApi } = await import("../lib/calendarsApi");
const { useAuthStore } = await import("../lib/authStore");
const { useCalendarsStore } = await import("../lib/calendarsStore");
const { useWorkspacesStore } = await import("../lib/workspacesStore");
const { CalendarPickerModal } = await import("./CalendarPickerModal");

const primary = {
  id: "primary",
  name: "someone@gmail.com",
  color: "#0B8043FF",
  selected: true,
  writable: true,
};
const sharedIn = {
  id: "shared-in@group.calendar.google.com",
  name: "Team calendar",
  color: "#7627BBFF",
  selected: false,
  writable: false,
};

beforeEach(() => {
  vi.clearAllMocks();
  useAuthStore.setState({ accessToken: "token-123" });
  useCalendarsStore.setState({ calendars: [] });
  useWorkspacesStore.setState({ activeWorkspaceId: 7 });
  vi.mocked(calendarsApi.list).mockResolvedValue([]);
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
});
