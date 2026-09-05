import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("./connectionsApi", async () => {
  const actual = await vi.importActual<typeof import("./connectionsApi")>("./connectionsApi");
  return {
    ...actual,
    connectionsApi: {
      list: vi.fn(),
      connectGoogle: vi.fn(),
      disconnect: vi.fn(),
    },
  };
});

const { connectionsApi } = await import("./connectionsApi");
const { useConnectionsStore } = await import("./connectionsStore");
const { useAuthStore } = await import("./authStore");

const connectionA = {
  id: 1,
  provider: "google",
  accountEmail: "work@gmail.com",
  status: "live" as const,
  createdAt: "2026-01-01T00:00:00Z",
};
const connectionB = {
  id: 2,
  provider: "google",
  accountEmail: "personal@gmail.com",
  status: "live" as const,
  createdAt: "2026-01-02T00:00:00Z",
};

beforeEach(() => {
  vi.clearAllMocks();
  useConnectionsStore.setState({ connections: [] });
  useAuthStore.setState({ accessToken: "token-123" });
});

describe("fetchConnections", () => {
  it("throws when there is no access token", async () => {
    useAuthStore.setState({ accessToken: null });

    await expect(useConnectionsStore.getState().fetchConnections()).rejects.toThrow(
      "Not authenticated.",
    );
  });

  it("stores the fetched list", async () => {
    vi.mocked(connectionsApi.list).mockResolvedValue([connectionA, connectionB]);

    await useConnectionsStore.getState().fetchConnections();

    expect(useConnectionsStore.getState().connections).toEqual([connectionA, connectionB]);
    expect(connectionsApi.list).toHaveBeenCalledWith("token-123");
  });
});

describe("connectGoogle", () => {
  it("returns the authorize url without touching the stored list", async () => {
    vi.mocked(connectionsApi.connectGoogle).mockResolvedValue("https://accounts.google.com/authorize");

    const url = await useConnectionsStore.getState().connectGoogle();

    expect(url).toBe("https://accounts.google.com/authorize");
    expect(connectionsApi.connectGoogle).toHaveBeenCalledWith("token-123");
  });
});

describe("disconnect", () => {
  it("removes only the disconnected connection", async () => {
    useConnectionsStore.setState({ connections: [connectionA, connectionB] });
    vi.mocked(connectionsApi.disconnect).mockResolvedValue(undefined);

    await useConnectionsStore.getState().disconnect(connectionA.id);

    expect(useConnectionsStore.getState().connections).toEqual([connectionB]);
    expect(connectionsApi.disconnect).toHaveBeenCalledWith("token-123", connectionA.id);
  });
});
