import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router";
import type { Calendar } from "../../lib/calendar";

// Same convention as the other component tests: the *Api modules are mocked,
// the stores are real.
vi.mock("../../lib/calendarsApi", () => ({
  calendarsApi: { list: vi.fn(), setExposure: vi.fn(async () => true) },
}));
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
vi.mock("../../lib/calendarSetsApi", async () => {
  const actual = await vi.importActual<typeof import("../../lib/calendarSetsApi")>("../../lib/calendarSetsApi");
  return {
    ...actual,
    calendarSetsApi: { ...actual.calendarSetsApi, addCalendar: vi.fn(), removeCalendar: vi.fn() },
  };
});

const { useAuthStore } = await import("../../lib/authStore");
const { useCalendarsStore } = await import("../../lib/calendarsStore");
const { useEventsStore } = await import("../../lib/eventsStore");
const { useShellStore } = await import("../../lib/shellStore");
const { useWorkspacesStore } = await import("../../lib/workspacesStore");
const { useConnectionsStore } = await import("../../lib/connectionsStore");
const { useCalendarSetsStore } = await import("../../lib/calendarSetsStore");
const { connectionsApi } = await import("../../lib/connectionsApi");
const { calendarsApi } = await import("../../lib/calendarsApi");
const { calendarSetsApi } = await import("../../lib/calendarSetsApi");
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
  useShellStore.setState({ checkedCalendarIds: new Set<string>(), activeCalendarSetId: null });
  useConnectionsStore.setState({ connections: [] });
  useCalendarSetsStore.setState({ calendarSets: [] });
});

// CalendarList now navigates to Settings' Calendar sets Section from the
// empty-Set state (#306), so every render needs a Router context for
// useNavigate to work. The destination route is a stand-in, not the real
// Section — CalendarList only needs to prove it asked to go there.
function renderCalendarList() {
  return render(
    <MemoryRouter>
      <Routes>
        <Route path="/" element={<CalendarList />} />
        <Route path="/settings/calendar-sets" element={<div>Calendar sets destination</div>} />
      </Routes>
    </MemoryRouter>,
  );
}

async function openMenu(calendarName: string) {
  await userEvent.click(screen.getByRole("button", { name: `${calendarName} actions` }));
}

describe("CalendarList row menu", () => {
  it("offers Share on a calendar the viewer owns", async () => {
    useCalendarsStore.setState({ calendars: [owned] });
    renderCalendarList();

    await openMenu("Personal");

    expect(await screen.findByRole("menuitem", { name: "Share" })).toBeInTheDocument();
  });

  it("omits Share entirely on a calendar the viewer only has Editor Access to", async () => {
    useCalendarsStore.setState({ calendars: [editorAccess] });
    renderCalendarList();

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
    renderCalendarList();

    expect(screen.getByText("work@gmail.com")).toBeInTheDocument();
    expect(screen.getByText("personal@gmail.com")).toBeInTheDocument();
    expect(screen.getByText("Work")).toBeInTheDocument();
    expect(screen.getByText("Personal (Google)")).toBeInTheDocument();
  });

  it("keeps a Linked Calendar out of My calendars", () => {
    useCalendarsStore.setState({ calendars: [owned, workLinked] });
    renderCalendarList();

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
    renderCalendarList();

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
    renderCalendarList();

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
    renderCalendarList();

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
    renderCalendarList();

    expect(screen.getAllByText("work@gmail.com")).toHaveLength(1);
    expect(screen.queryByText("Unknown account")).not.toBeInTheDocument();
  });

  // #288: a Linked Calendar can be refreshed on demand, exactly like a
  // Subscribed one — and, like one, offers no per-Calendar Export.
  it("offers Refresh and not Export on a Linked Calendar", async () => {
    useCalendarsStore.setState({ calendars: [workLinked] });
    renderCalendarList();

    await openMenu("Work");

    expect(await screen.findByRole("menuitem", { name: "Refresh" })).toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: "Export" })).not.toBeInTheDocument();
  });
});

// #297, ADR-0080: Exposure's toggle lives in a Linked Calendar's row menu —
// the one surface both its Owner and an accessor it was Shared to reach.
describe("Linked Calendar Exposure toggle", () => {
  const ownedLinked: Calendar = {
    id: "cal-owned-linked",
    name: "Work",
    color: "#8E44ADFF",
    access: "viewer",
    isOwner: true,
    sourceKind: "connection",
    connectionId: 1,
    connectionAccountEmail: "work@gmail.com",
    exposed: false,
  };
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
    exposed: true,
  };

  it("offers the toggle to the Linked Calendar's own Owner, reflecting the resolved default", async () => {
    useCalendarsStore.setState({ calendars: [ownedLinked] });
    renderCalendarList();

    await openMenu("Work");

    const item = await screen.findByRole("menuitemcheckbox", { name: "Show on my devices" });
    expect(item).toHaveAttribute("aria-checked", "false");
  });

  it("also offers the toggle to an accessor it was Shared to, reflecting their own resolved default", async () => {
    useCalendarsStore.setState({ calendars: [owned, sharedLinked] });
    renderCalendarList();

    await openMenu("Colleague's Google");

    const item = await screen.findByRole("menuitemcheckbox", { name: "Show on my devices" });
    expect(item).toHaveAttribute("aria-checked", "true");
  });

  it("omits the toggle on a Calendar that isn't Linked", async () => {
    useCalendarsStore.setState({ calendars: [owned] });
    renderCalendarList();

    await openMenu("Personal");

    expect(screen.queryByRole("menuitemcheckbox", { name: "Show on my devices" })).not.toBeInTheDocument();
  });

  it("writes the caller's own choice when toggled", async () => {
    useCalendarsStore.setState({ calendars: [ownedLinked] });
    renderCalendarList();

    await openMenu("Work");
    await userEvent.click(await screen.findByRole("menuitemcheckbox", { name: "Show on my devices" }));

    expect(calendarsApi.setExposure).toHaveBeenCalledWith("token-123", "cal-owned-linked", true);
    expect(useCalendarsStore.getState().calendars.find((c) => c.id === "cal-owned-linked")?.exposed).toBe(true);
  });
});

// #303, ADR-0082: the sidebar narrows to the Active Calendar Set before
// grouping, so an out-of-Set Calendar is absent rather than unchecked.
describe("Calendar Set narrowing", () => {
  const workCalendar: Calendar = {
    id: "cal-work",
    name: "Work",
    color: "#8E44ADFF",
    access: "owner",
    isOwner: true,
  };
  const personalCalendar: Calendar = {
    id: "cal-personal",
    name: "Personal",
    color: "#3498DBFF",
    access: "owner",
    isOwner: true,
  };
  const feedCalendar: Calendar = {
    id: "cal-feed",
    name: "Holidays",
    color: "#2ECC71FF",
    access: "owner",
    isOwner: true,
    sourceUrl: "https://example.com/feed.ics",
  };
  const workSet = { id: 1, name: "Work", calendarIds: ["cal-work"] };

  it("renders an out-of-Set Calendar's row as absent, not merely unchecked", () => {
    useCalendarsStore.setState({ calendars: [workCalendar, personalCalendar] });
    useCalendarSetsStore.setState({ calendarSets: [workSet] });
    useShellStore.setState({
      checkedCalendarIds: new Set(["cal-work", "cal-personal"]),
      activeCalendarSetId: workSet.id,
    });
    renderCalendarList();

    expect(screen.getByText("Work")).toBeInTheDocument();
    expect(screen.queryByText("Personal")).not.toBeInTheDocument();
    expect(screen.queryByRole("checkbox", { name: "Personal" })).not.toBeInTheDocument();
  });

  it("keeps a surviving heading, filtered to its in-Set Calendars", () => {
    useCalendarsStore.setState({ calendars: [workCalendar, personalCalendar] });
    useCalendarSetsStore.setState({ calendarSets: [workSet] });
    useShellStore.setState({
      checkedCalendarIds: new Set(["cal-work", "cal-personal"]),
      activeCalendarSetId: workSet.id,
    });
    renderCalendarList();

    expect(screen.getByText("My calendars")).toBeInTheDocument();
  });

  it("hides a heading with no in-Set Calendars left under it", () => {
    useCalendarsStore.setState({ calendars: [workCalendar, feedCalendar] });
    useCalendarSetsStore.setState({ calendarSets: [workSet] });
    useShellStore.setState({
      checkedCalendarIds: new Set(["cal-work", "cal-feed"]),
      activeCalendarSetId: workSet.id,
    });
    renderCalendarList();

    expect(screen.getByText("My calendars")).toBeInTheDocument();
    expect(screen.queryByText("Subscribed calendars")).not.toBeInTheDocument();
  });

  it("keeps every heading, even an empty one, while All calendars is active", () => {
    useCalendarsStore.setState({ calendars: [workCalendar] });
    useCalendarSetsStore.setState({ calendarSets: [workSet] });
    useShellStore.setState({
      checkedCalendarIds: new Set(["cal-work"]),
      activeCalendarSetId: null,
    });
    renderCalendarList();

    expect(screen.getByText("My calendars")).toBeInTheDocument();
    expect(screen.getByText("Subscribed calendars")).toBeInTheDocument();
  });

  it("keeps a Calendar's toggle value across a Set switch and back", async () => {
    useCalendarsStore.setState({ calendars: [workCalendar, personalCalendar] });
    useCalendarSetsStore.setState({ calendarSets: [workSet] });
    useShellStore.setState({
      checkedCalendarIds: new Set(["cal-work", "cal-personal"]),
      activeCalendarSetId: null,
    });
    renderCalendarList();

    await userEvent.click(screen.getByRole("checkbox", { name: "Personal" }));
    expect(useShellStore.getState().checkedCalendarIds.has("cal-personal")).toBe(false);

    // Switching into a Set that excludes "Personal" hides its row entirely;
    // switching back to All calendars must not have touched its checked
    // value while it was gone.
    useShellStore.setState({ activeCalendarSetId: workSet.id });
    useShellStore.setState({ activeCalendarSetId: null });

    expect(useShellStore.getState().checkedCalendarIds.has("cal-personal")).toBe(false);
    expect(screen.getByRole("checkbox", { name: "Personal" })).toHaveAttribute("aria-checked", "false");
  });
});

// #306, ADR-0082: an empty Active Calendar Set — created empty, or emptied by
// cascade when a Share was revoked or a Calendar deleted — names itself in
// the sidebar rather than rendering nothing, and offers both a route to
// manage Sets and a way back to "All calendars". The grid renders nothing in
// this state too, via the same inScopeCalendars answer CalendarList itself
// uses; that absence needs no message of its own.
describe("Empty Calendar Set", () => {
  const emptySet = { id: 2, name: "Empty", calendarIds: [] };
  const workCalendar: Calendar = {
    id: "cal-work",
    name: "Work",
    color: "#8E44ADFF",
    access: "owner",
    isOwner: true,
  };

  function activateEmptySet() {
    useCalendarsStore.setState({ calendars: [workCalendar] });
    useCalendarSetsStore.setState({ calendarSets: [emptySet] });
    useShellStore.setState({
      checkedCalendarIds: new Set(["cal-work"]),
      activeCalendarSetId: emptySet.id,
    });
  }

  it("names the situation instead of rendering nothing", () => {
    activateEmptySet();
    renderCalendarList();

    expect(screen.getByText(`"${emptySet.name}" has no calendars in it.`)).toBeInTheDocument();
    expect(screen.queryByText("My calendars")).not.toBeInTheDocument();
    expect(screen.queryByText("Work")).not.toBeInTheDocument();
  });

  it("offers a route to the Calendar Sets Settings Section", async () => {
    activateEmptySet();
    renderCalendarList();

    await userEvent.click(screen.getByRole("button", { name: "Manage sets" }));

    expect(await screen.findByText("Calendar sets destination")).toBeInTheDocument();
  });

  it("offers a way back to All calendars", async () => {
    activateEmptySet();
    renderCalendarList();

    await userEvent.click(screen.getByRole("button", { name: "Back to All calendars" }));

    expect(useShellStore.getState().activeCalendarSetId).toBeNull();
    expect(await screen.findByText("My calendars")).toBeInTheDocument();
  });
});

// #307, ADR-0082: a Calendar can be put into a Calendar Set straight from its
// sidebar row menu, without opening Settings — the same immediate-effect
// toggle CalendarSetMembershipDialog offers, reached from where the intent
// actually arises.
describe("Add to set", () => {
  const workSet = { id: 1, name: "Work", calendarIds: ["cal-1"] };
  const weekendSet = { id: 2, name: "Weekend", calendarIds: [] };
  const linked: Calendar = {
    id: "cal-linked",
    name: "Google Work",
    color: "#8E44ADFF",
    access: "viewer",
    isOwner: true,
    sourceKind: "connection",
    connectionId: 1,
    connectionAccountEmail: "work@gmail.com",
  };

  async function openAddToSet(calendarName: string) {
    await openMenu(calendarName);
    await userEvent.click(await screen.findByRole("menuitem", { name: "Add to set" }));
  }

  it("is absent with zero Sets rather than an empty submenu", async () => {
    useCalendarsStore.setState({ calendars: [owned] });
    useCalendarSetsStore.setState({ calendarSets: [] });
    renderCalendarList();

    await openMenu("Personal");

    expect(await screen.findByRole("menuitem", { name: "Edit" })).toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: "Add to set" })).not.toBeInTheDocument();
  });

  it("lists the caller's Sets, marking the ones this Calendar already belongs to", async () => {
    useCalendarsStore.setState({ calendars: [owned] });
    useCalendarSetsStore.setState({ calendarSets: [workSet, weekendSet] });
    renderCalendarList();

    await openAddToSet("Personal");

    expect(await screen.findByRole("menuitemcheckbox", { name: "Remove Personal from Work" })).toHaveAttribute(
      "aria-checked",
      "true",
    );
    expect(screen.getByRole("menuitemcheckbox", { name: "Add Personal to Weekend" })).toHaveAttribute(
      "aria-checked",
      "false",
    );
  });

  it("adds membership immediately when an unmarked Set is selected", async () => {
    useCalendarsStore.setState({ calendars: [owned] });
    useCalendarSetsStore.setState({ calendarSets: [weekendSet] });
    vi.mocked(calendarSetsApi.addCalendar).mockResolvedValue(undefined);
    renderCalendarList();

    await openAddToSet("Personal");
    await userEvent.click(await screen.findByRole("menuitemcheckbox", { name: "Add Personal to Weekend" }));

    expect(calendarSetsApi.addCalendar).toHaveBeenCalledWith("token-123", weekendSet.id, "cal-1");
    expect(useCalendarSetsStore.getState().calendarSets[0].calendarIds).toContain("cal-1");
  });

  it("removes membership immediately when a marked Set is selected", async () => {
    useCalendarsStore.setState({ calendars: [owned] });
    useCalendarSetsStore.setState({ calendarSets: [workSet] });
    vi.mocked(calendarSetsApi.removeCalendar).mockResolvedValue(undefined);
    renderCalendarList();

    await openAddToSet("Personal");
    await userEvent.click(await screen.findByRole("menuitemcheckbox", { name: "Remove Personal from Work" }));

    expect(calendarSetsApi.removeCalendar).toHaveBeenCalledWith("token-123", workSet.id, "cal-1");
    expect(useCalendarSetsStore.getState().calendarSets[0].calendarIds).not.toContain("cal-1");
  });

  it("works identically on a shared-in row", async () => {
    useCalendarsStore.setState({ calendars: [editorAccess] });
    useCalendarSetsStore.setState({ calendarSets: [weekendSet] });
    renderCalendarList();

    await openMenu("Team");

    expect(await screen.findByRole("menuitem", { name: "Add to set" })).toBeInTheDocument();
  });

  it("works identically on a Linked Calendar row", async () => {
    useCalendarsStore.setState({ calendars: [linked] });
    useCalendarSetsStore.setState({ calendarSets: [weekendSet] });
    renderCalendarList();

    await openMenu("Google Work");

    expect(await screen.findByRole("menuitem", { name: "Add to set" })).toBeInTheDocument();
  });

  it("works identically on a Subscribed Calendar row", async () => {
    const subscribed: Calendar = {
      id: "cal-subscribed",
      name: "Holidays",
      color: "#2ECC71FF",
      access: "owner",
      isOwner: true,
      sourceUrl: "https://example.com/feed.ics",
    };
    useCalendarsStore.setState({ calendars: [subscribed] });
    useCalendarSetsStore.setState({ calendarSets: [weekendSet] });
    renderCalendarList();

    await openMenu("Holidays");

    expect(await screen.findByRole("menuitem", { name: "Add to set" })).toBeInTheDocument();
  });
});
