import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Calendar } from "../../lib/calendar";

// Same convention as the other component tests: the *Api modules are mocked,
// the stores are real.
vi.mock("../../lib/calendarsApi", () => ({ calendarsApi: { list: vi.fn() } }));
vi.mock("../../lib/connectionsApi", async () => {
  const actual = await vi.importActual<typeof import("../../lib/connectionsApi")>("../../lib/connectionsApi");
  return {
    ...actual,
    connectionsApi: { list: vi.fn(async () => []), listPickerCalendars: vi.fn(), importCalendars: vi.fn() },
  };
});
vi.mock("../../lib/icsApi", () => ({
  icsApi: { downloadCalendar: vi.fn(), calendarOversizedAttachments: vi.fn() },
}));
vi.mock("../../lib/toast", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));

const { useAuthStore } = await import("../../lib/authStore");
const { useCalendarsStore } = await import("../../lib/calendarsStore");
const { useEventsStore } = await import("../../lib/eventsStore");
const { useShellStore } = await import("../../lib/shellStore");
const { useWorkspacesStore } = await import("../../lib/workspacesStore");
const { useConnectionsStore } = await import("../../lib/connectionsStore");
const { connectionsApi } = await import("../../lib/connectionsApi");
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
  useConnectionsStore.setState({ connections: [] });
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

// #286: a Linked Calendar groups under a heading labelled with its
// Connection's account Email, distinct from "My calendars" and from every
// other Connection's own heading.
describe("Linked Calendar grouping", () => {
  const workLinked: Calendar = {
    id: "cal-work",
    name: "Work",
    color: "#8E44ADFF",
    access: "viewer",
    isOwner: true,
    sourceKind: "connection",
    connectionId: 1,
    connectionAccountEmail: "work@gmail.com",
  };
  const personalLinked: Calendar = {
    id: "cal-personal",
    name: "Personal (Google)",
    color: "#3498DBFF",
    access: "viewer",
    isOwner: true,
    sourceKind: "connection",
    connectionId: 2,
    connectionAccountEmail: "personal@gmail.com",
  };

  it("shows one heading per Connection's account email", () => {
    useCalendarsStore.setState({ calendars: [owned, workLinked, personalLinked] });
    render(<CalendarList />);

    expect(screen.getByText("work@gmail.com")).toBeInTheDocument();
    expect(screen.getByText("personal@gmail.com")).toBeInTheDocument();
    expect(screen.getByText("Work")).toBeInTheDocument();
    expect(screen.getByText("Personal (Google)")).toBeInTheDocument();
  });

  it("keeps a Linked Calendar out of My calendars", () => {
    useCalendarsStore.setState({ calendars: [owned, workLinked] });
    render(<CalendarList />);

    expect(screen.getByText("My calendars")).toBeInTheDocument();
    // "Work" only ever renders under its Connection's own heading.
    expect(screen.getAllByText("Work")).toHaveLength(1);
  });

  // #295: the Connection's sidebar heading re-opens the Calendar picker, so a
  // User can add a calendar they skipped from where they actually are.
  it("re-opens the Calendar picker from the Connection heading", async () => {
    useWorkspacesStore.setState({ activeWorkspaceId: 7 });
    vi.mocked(connectionsApi.listPickerCalendars).mockResolvedValue([]);
    useCalendarsStore.setState({ calendars: [{ ...workLinked, connectionId: 42 }] });
    render(<CalendarList />);

    await userEvent.click(
      screen.getByRole("button", { name: "Choose calendars from work@gmail.com" }),
    );

    expect(await screen.findByText("Choose calendars to bring in")).toBeInTheDocument();
    useWorkspacesStore.setState({ activeWorkspaceId: null });
  });

  // #294: a Linked Calendar Shared to the viewer appears under "Shared with
  // me", never under a Connection heading — those headings are the Owner's
  // account groupings, and the viewer is not the one who connected Google.
  it("puts a Linked Calendar shared to the viewer under Shared with me", () => {
    const sharedLinked: Calendar = {
      id: "cal-shared-linked",
      name: "Colleague's Google",
      color: "#3498DBFF",
      access: "viewer",
      isOwner: false,
      ownerName: "Bob",
      sourceKind: "connection",
      connectionId: 3,
      connectionAccountEmail: "bob@gmail.com",
    };
    useCalendarsStore.setState({ calendars: [owned, sharedLinked] });
    render(<CalendarList />);

    expect(screen.getByText("Shared with me")).toBeInTheDocument();
    // No Connection heading is rendered — that grouping is the Owner's, and
    // the viewer did not connect this account.
    expect(screen.queryByText("bob@gmail.com")).not.toBeInTheDocument();
    expect(screen.getAllByText("Colleague's Google")).toHaveLength(1);
    expect(screen.getByText("Shared by Bob")).toBeInTheDocument();
  });

  // The grouping keys on the Connection id, never on the account Email. The
  // Email is populated only by the *list* endpoint's batched join; the
  // single-Calendar GET that refreshCalendar re-reads a Linked Calendar with
  // omits it, so a Calendar that has just been refreshed arrives back in the
  // store with connectionAccountEmail undefined. Keying on the Email dropped
  // it into an "Unknown account" heading until an unrelated list fetch put the
  // Email back — visible as the Calendar jumping between headings for a few
  // seconds after every Refresh.
  it("keeps a refreshed Linked Calendar in its Connection's group when the account email is missing", () => {
    useConnectionsStore.setState({
      connections: [
        { id: 1, provider: "google", accountEmail: "work@gmail.com", status: "live", createdAt: "2026-01-01T00:00:00Z" },
      ],
    });
    // Exactly what calendarsApi.get returns: the Connection id, no Email.
    const refreshed: Calendar = { ...workLinked, connectionAccountEmail: undefined };
    useCalendarsStore.setState({ calendars: [owned, refreshed] });
    render(<CalendarList />);

    expect(screen.getByText("work@gmail.com")).toBeInTheDocument();
    expect(screen.queryByText("Unknown account")).not.toBeInTheDocument();
    expect(screen.getByText("Work")).toBeInTheDocument();
  });

  // The two halves of one Connection must not split across two headings just
  // because one of them was refreshed and the other was not — the case the
  // screenshots showed, with a single account rendering twice.
  it("keeps a refreshed and an unrefreshed Linked Calendar under one heading", () => {
    useConnectionsStore.setState({
      connections: [
        { id: 1, provider: "google", accountEmail: "work@gmail.com", status: "live", createdAt: "2026-01-01T00:00:00Z" },
      ],
    });
    const sibling: Calendar = { ...workLinked, id: "cal-work-2", name: "Work Two" };
    const refreshed: Calendar = { ...workLinked, connectionAccountEmail: undefined };
    useCalendarsStore.setState({ calendars: [refreshed, sibling] });
    render(<CalendarList />);

    expect(screen.getAllByText("work@gmail.com")).toHaveLength(1);
    expect(screen.queryByText("Unknown account")).not.toBeInTheDocument();
  });

  // #288: a Linked Calendar can be refreshed on demand, exactly like a
  // Subscribed one — and, like one, offers no per-Calendar Export.
  it("offers Refresh and not Export on a Linked Calendar", async () => {
    useCalendarsStore.setState({ calendars: [workLinked] });
    render(<CalendarList />);

    await openMenu("Work");

    expect(await screen.findByRole("menuitem", { name: "Refresh" })).toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: "Export" })).not.toBeInTheDocument();
  });
});
