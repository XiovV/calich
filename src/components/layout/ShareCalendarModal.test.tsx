import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Calendar } from "../../lib/calendar";
import type {
  GroupShare,
  Share,
  ShareTargets,
  ShareTargetUser,
} from "../../lib/calendarsApi";

// Same convention as the other component tests: the *Api modules are mocked,
// the stores are real — so a grant here runs the real calendarsStore write
// (which re-fetches Calendars) and only stops at the HTTP boundary.
vi.mock("../../lib/calendarsApi", () => ({
  calendarsApi: {
    list: vi.fn(),
    listShares: vi.fn(),
    listGroupShares: vi.fn(),
    shareTargets: vi.fn(),
    share: vi.fn(),
    shareWithGroup: vi.fn(),
    revokeShare: vi.fn(),
    revokeGroupShare: vi.fn(),
  },
}));
vi.mock("../../lib/toast", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

const { calendarsApi } = await import("../../lib/calendarsApi");
const { toast } = await import("../../lib/toast");
const { useAuthStore } = await import("../../lib/authStore");
const { useCalendarsStore } = await import("../../lib/calendarsStore");
const { useWorkspacesStore } = await import("../../lib/workspacesStore");
const { ShareCalendarModal } = await import("./ShareCalendarModal");

const calendar: Calendar = {
  id: "cal-1",
  name: "Personal",
  color: "#8E44ADFF",
  access: "owner",
  isOwner: true,
};

const bobShare: Share = {
  userId: 2,
  name: "Bob",
  email: "bob@example.com",
  role: "viewer",
  createdAt: "2026-01-01T00:00:00Z",
};
const designShare: GroupShare = {
  groupId: 5,
  groupName: "Design",
  role: "editor",
  createdAt: "2026-01-01T00:00:00Z",
};
const carolTarget: ShareTargetUser = { userId: 3, name: "Carol", email: "carol@example.com" };

const noTargets: ShareTargets = { users: [], groups: [] };

function seedSharing({
  shares = [] as Share[],
  groupShares = [] as GroupShare[],
  targets = noTargets,
} = {}) {
  vi.mocked(calendarsApi.listShares).mockResolvedValue(shares);
  vi.mocked(calendarsApi.listGroupShares).mockResolvedValue(groupShares);
  vi.mocked(calendarsApi.shareTargets).mockResolvedValue(targets);
  vi.mocked(calendarsApi.list).mockResolvedValue([calendar]);
}

beforeEach(() => {
  vi.clearAllMocks();
  useAuthStore.setState({ accessToken: "token-123" });
  useCalendarsStore.setState({ calendars: [calendar] });
  useWorkspacesStore.setState({ activeWorkspaceId: 7, workspaces: [] });
});

describe("ShareCalendarModal chrome", () => {
  it("names the calendar in its title and offers Done as its only footer button", async () => {
    seedSharing();
    const onClose = vi.fn();
    render(<ShareCalendarModal calendar={calendar} onClose={onClose} />);

    expect(await screen.findByText('Share "Personal"')).toBeInTheDocument();

    // A surface you operate, not a form you submit (ADR-0068): every control
    // in here commits on click, so there is nothing for a Save to commit and
    // nothing for a Cancel to undo.
    expect(screen.queryByRole("button", { name: "Save" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Cancel" })).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Done" }));
    expect(onClose).toHaveBeenCalled();
  });
});

describe("ShareCalendarModal roster", () => {
  it("lists Users and Groups that have Access, marking which rows are groups", async () => {
    seedSharing({ shares: [bobShare], groupShares: [designShare] });
    render(<ShareCalendarModal calendar={calendar} onClose={vi.fn()} />);

    const roster = await screen.findByRole("list");
    expect(within(roster).getByText("Bob")).toBeInTheDocument();
    expect(within(roster).getByText("Design")).toBeInTheDocument();
    expect(within(roster).getByText("(group)")).toBeInTheDocument();
    expect(within(roster).getByLabelText("Bob's role")).toHaveTextContent("Viewer");
    expect(within(roster).getByLabelText("Design's role")).toHaveTextContent("Editor");
  });

  it("says nobody has Access when there are no shares at all", async () => {
    seedSharing({ targets: { users: [carolTarget], groups: [] } });
    render(<ShareCalendarModal calendar={calendar} onClose={vi.fn()} />);

    expect(
      await screen.findByText("Nobody else has Access to this calendar yet."),
    ).toBeInTheDocument();
    expect(screen.queryByRole("list")).not.toBeInTheDocument();
  });

  it("says everyone already has Access when every target is already shared with", async () => {
    seedSharing({
      shares: [bobShare],
      targets: { users: [{ userId: 2, name: "Bob", email: "bob@example.com" }], groups: [] },
    });
    render(<ShareCalendarModal calendar={calendar} onClose={vi.fn()} />);

    expect(
      await screen.findByText(
        "Everyone and every group in this workspace already has Access.",
      ),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Add" })).not.toBeInTheDocument();
  });
});

// #294: a Linked Calendar's Shares are clamped to Viewer (ADR-0075) — an
// Editor's write would run as the connecting User's own Provider identity.
// The Role picker never offers Editor here, and a new grant is Viewer even
// where the Workspace defaults new Shares to Editor.
describe("ShareCalendarModal on a linked calendar", () => {
  const linkedCalendar: Calendar = {
    id: "cal-linked",
    name: "Work (Google)",
    color: "#8E44ADFF",
    access: "viewer",
    isOwner: true,
    sourceKind: "connection",
    connectionAccountEmail: "work@gmail.com",
  };

  it("grants as Viewer even when the workspace defaults new shares to Editor", async () => {
    useWorkspacesStore.setState({
      activeWorkspaceId: 7,
      workspaces: [{ id: 7, name: "Team", defaultSharePrivacy: "workspace" }],
    });
    useCalendarsStore.setState({ calendars: [linkedCalendar] });
    vi.mocked(calendarsApi.listShares).mockResolvedValue([]);
    vi.mocked(calendarsApi.listGroupShares).mockResolvedValue([]);
    vi.mocked(calendarsApi.shareTargets).mockResolvedValue({ users: [carolTarget], groups: [] });
    vi.mocked(calendarsApi.list).mockResolvedValue([linkedCalendar]);
    vi.mocked(calendarsApi.share).mockResolvedValue({
      userId: 3,
      name: "Carol",
      email: "carol@example.com",
      role: "viewer",
      createdAt: "2026-02-01T00:00:00Z",
    });
    render(<ShareCalendarModal calendar={linkedCalendar} onClose={vi.fn()} />);

    expect(
      await screen.findByText("A linked calendar can only be shared with the viewer role."),
    ).toBeInTheDocument();

    await userEvent.click(await screen.findByRole("button", { name: "Add" }));

    expect(calendarsApi.share).toHaveBeenCalledWith(
      "token-123",
      "cal-linked",
      "carol@example.com",
      "viewer",
    );
  });

  it("offers only Viewer in the new-share Role picker", async () => {
    useCalendarsStore.setState({ calendars: [linkedCalendar] });
    vi.mocked(calendarsApi.listShares).mockResolvedValue([]);
    vi.mocked(calendarsApi.listGroupShares).mockResolvedValue([]);
    vi.mocked(calendarsApi.shareTargets).mockResolvedValue({ users: [carolTarget], groups: [] });
    vi.mocked(calendarsApi.list).mockResolvedValue([linkedCalendar]);
    render(<ShareCalendarModal calendar={linkedCalendar} onClose={vi.fn()} />);

    const roleSelect = await screen.findByLabelText("Role");
    await userEvent.click(roleSelect);

    expect(await screen.findByRole("option", { name: "Viewer" })).toBeInTheDocument();
    expect(screen.queryByRole("option", { name: "Editor" })).not.toBeInTheDocument();
  });
});

describe("ShareCalendarModal writes", () => {
  it("appends a granted Share to the roster and drops it from the picker", async () => {
    seedSharing({ targets: { users: [carolTarget], groups: [] } });
    vi.mocked(calendarsApi.share).mockResolvedValue({
      userId: 3,
      name: "Carol",
      email: "carol@example.com",
      role: "viewer",
      createdAt: "2026-02-01T00:00:00Z",
    });
    render(<ShareCalendarModal calendar={calendar} onClose={vi.fn()} />);

    await userEvent.click(await screen.findByRole("button", { name: "Add" }));

    const roster = await screen.findByRole("list");
    expect(within(roster).getByText("Carol")).toBeInTheDocument();
    expect(calendarsApi.share).toHaveBeenCalledWith(
      "token-123",
      "cal-1",
      "carol@example.com",
      "viewer",
    );
    // Carol was the only target, so the picker has nobody left to offer.
    await waitFor(() =>
      expect(
        screen.getByText("Everyone and every group in this workspace already has Access."),
      ).toBeInTheDocument(),
    );
  });

  it("puts a revoked Share back and toasts when the revoke fails", async () => {
    seedSharing({ shares: [bobShare] });
    vi.mocked(calendarsApi.revokeShare).mockRejectedValue(new Error("nope"));
    render(<ShareCalendarModal calendar={calendar} onClose={vi.fn()} />);

    await userEvent.click(
      await screen.findByRole("button", { name: "Remove Bob's access" }),
    );

    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith("Failed to remove Bob's access."),
    );
    expect(within(screen.getByRole("list")).getByText("Bob")).toBeInTheDocument();
  });
});
