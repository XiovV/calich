import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";

// Same convention as the other component tests: the *Api module is mocked,
// the stores are real (#234) — this covers the Settings → Connections
// wiring, not the request/response shape itself, which connectionsApi.test.ts
// already owns.
vi.mock("../lib/connectionsApi", async () => {
  const actual = await vi.importActual<typeof import("../lib/connectionsApi")>("../lib/connectionsApi");
  return {
    ...actual,
    connectionsApi: {
      list: vi.fn(),
      connectGoogle: vi.fn(),
      disconnect: vi.fn(),
      listPickerCalendars: vi.fn(),
      importCalendars: vi.fn(),
    },
  };
});

const { connectionsApi } = await import("../lib/connectionsApi");
const { useAuthStore } = await import("../lib/authStore");
const { useConnectionsStore } = await import("../lib/connectionsStore");
const { useWorkspacesStore } = await import("../lib/workspacesStore");
const { ConnectionsSection } = await import("./ConnectionsSection");

const user = {
  id: 1,
  name: "Ada",
  mustChangePassword: false,
  email: "ada@example.com",
  emailReminderChannelAvailable: false,
  googleProviderAvailable: true,
  invitationRepliesConfigured: false,
  syncedDeviceRemindersEnabled: false,
  weekStart: 1,
  defaultView: "week" as const,
  timeFormat: "24h" as const,
  workingHoursStart: null,
  workingHoursEnd: null,
};

const connectionA = {
  id: 1,
  provider: "google",
  accountEmail: "work@gmail.com",
  status: "live" as const,
  createdAt: "2026-01-01T00:00:00Z",
};

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <ConnectionsSection />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  useAuthStore.setState({ status: "authenticated", user, accessToken: "token-123" });
  useConnectionsStore.setState({ connections: [] });
  useWorkspacesStore.setState({ activeWorkspaceId: 7 });
  vi.mocked(connectionsApi.list).mockResolvedValue([]);
  vi.mocked(connectionsApi.listPickerCalendars).mockResolvedValue([]);
  Object.defineProperty(window, "location", {
    value: { ...window.location, href: "" },
    writable: true,
  });
});

afterEach(() => {
  useWorkspacesStore.setState({ activeWorkspaceId: null });
});

describe("ConnectionsSection — listing", () => {
  it("shows a placeholder when nothing is connected", async () => {
    renderAt("/settings/connections");

    expect(await screen.findByText("No accounts connected yet.")).toBeInTheDocument();
  });

  it("lists a connected account with its email and status", async () => {
    vi.mocked(connectionsApi.list).mockResolvedValue([connectionA]);
    renderAt("/settings/connections");

    expect(await screen.findByText("work@gmail.com")).toBeInTheDocument();
    expect(screen.getByText("Google · Connected")).toBeInTheDocument();
  });
});

describe("ConnectionsSection — Provider absent when unconfigured (#285, ADR-0051)", () => {
  it("offers Connect a Google account when the provider is configured", async () => {
    renderAt("/settings/connections");

    expect(
      await screen.findByRole("button", { name: "Connect a Google account" }),
    ).toBeInTheDocument();
  });

  it("hides the Connect button entirely when the provider is not configured", async () => {
    useAuthStore.setState({ user: { ...user, googleProviderAvailable: false } });
    renderAt("/settings/connections");

    await waitFor(() => expect(connectionsApi.list).toHaveBeenCalled());
    expect(screen.queryByRole("button", { name: "Connect a Google account" })).not.toBeInTheDocument();
    expect(
      screen.getByText(
        "This instance has no Google credentials configured, so connecting a Google account isn't available here.",
      ),
    ).toBeInTheDocument();
  });
});

describe("ConnectionsSection — connecting", () => {
  it("navigates the browser to the authorize url Connect returns", async () => {
    vi.mocked(connectionsApi.connectGoogle).mockResolvedValue("https://accounts.google.com/authorize");
    renderAt("/settings/connections");

    await userEvent.click(await screen.findByRole("button", { name: "Connect a Google account" }));

    await waitFor(() => expect(window.location.href).toBe("https://accounts.google.com/authorize"));
  });
});

describe("ConnectionsSection — the Google round trip's return (#285)", () => {
  it("shows a success banner and clears the query param after a successful connect", async () => {
    renderAt("/settings/connections?connected=1");

    expect(await screen.findByText("Google account connected.")).toBeInTheDocument();
  });

  it("shows a declined-consent message", async () => {
    renderAt("/settings/connections?connect_error=declined");

    expect(
      await screen.findByText("Google sign-in was cancelled before it finished."),
    ).toBeInTheDocument();
  });

  it("falls back to a generic message for an unrecognised error code", async () => {
    renderAt("/settings/connections?connect_error=something_unexpected");

    expect(await screen.findByText("Google couldn't be connected.")).toBeInTheDocument();
  });

  it("opens the Calendar picker for the connection the redirect names (#286)", async () => {
    renderAt("/settings/connections?connected=1&connection_id=42");

    expect(await screen.findByText("Choose calendars to bring in")).toBeInTheDocument();
    await waitFor(() => expect(connectionsApi.listPickerCalendars).toHaveBeenCalledWith("token-123", 42));
  });

  it("does not open the picker when connection_id is absent", async () => {
    renderAt("/settings/connections?connected=1");

    expect(await screen.findByText("Google account connected.")).toBeInTheDocument();
    expect(screen.queryByText("Choose calendars to bring in")).not.toBeInTheDocument();
  });
});

describe("ConnectionsSection — reconnecting a broken connection (#291)", () => {
  it("offers no Reconnect action for a live connection", async () => {
    vi.mocked(connectionsApi.list).mockResolvedValue([connectionA]);
    renderAt("/settings/connections");

    await screen.findByText("work@gmail.com");
    expect(screen.queryByRole("button", { name: "Reconnect" })).not.toBeInTheDocument();
  });

  it("offers a Reconnect action for an expired connection, driving the same OAuth flow", async () => {
    vi.mocked(connectionsApi.list).mockResolvedValue([{ ...connectionA, status: "expired" }]);
    vi.mocked(connectionsApi.connectGoogle).mockResolvedValue("https://accounts.google.com/authorize");
    renderAt("/settings/connections");

    expect(await screen.findByText("Google · Expired — reconnect to keep it syncing")).toBeInTheDocument();

    await userEvent.click(await screen.findByRole("button", { name: "Reconnect" }));

    await waitFor(() => expect(window.location.href).toBe("https://accounts.google.com/authorize"));
  });

  it("offers a Reconnect action for a revoked connection", async () => {
    vi.mocked(connectionsApi.list).mockResolvedValue([{ ...connectionA, status: "revoked" }]);
    renderAt("/settings/connections");

    expect(await screen.findByRole("button", { name: "Reconnect" })).toBeInTheDocument();
  });
});

describe("ConnectionsSection — disconnecting", () => {
  it("removes the connection after confirming", async () => {
    vi.mocked(connectionsApi.list).mockResolvedValue([connectionA]);
    vi.mocked(connectionsApi.disconnect).mockResolvedValue(undefined);
    vi.spyOn(window, "confirm").mockReturnValue(true);
    renderAt("/settings/connections");

    await userEvent.click(await screen.findByRole("button", { name: "Disconnect work@gmail.com" }));

    expect(connectionsApi.disconnect).toHaveBeenCalledWith("token-123", connectionA.id);
    await waitFor(() => expect(screen.getByText("No accounts connected yet.")).toBeInTheDocument());
  });

  it("does nothing when the confirmation is declined", async () => {
    vi.mocked(connectionsApi.list).mockResolvedValue([connectionA]);
    vi.spyOn(window, "confirm").mockReturnValue(false);
    renderAt("/settings/connections");

    await userEvent.click(await screen.findByRole("button", { name: "Disconnect work@gmail.com" }));

    expect(connectionsApi.disconnect).not.toHaveBeenCalled();
    expect(screen.getByText("work@gmail.com")).toBeInTheDocument();
  });
});
