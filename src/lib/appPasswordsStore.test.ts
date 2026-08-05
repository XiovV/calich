import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("./appPasswordsApi", async () => {
  const actual = await vi.importActual<typeof import("./appPasswordsApi")>("./appPasswordsApi");
  return {
    ...actual,
    appPasswordsApi: {
      list: vi.fn(),
      create: vi.fn(),
      revoke: vi.fn(),
    },
  };
});

const { appPasswordsApi } = await import("./appPasswordsApi");
const { useAppPasswordsStore } = await import("./appPasswordsStore");
const { useAuthStore } = await import("./authStore");

const appPasswordA = { id: 1, label: "iPhone", createdAt: "2026-01-01T00:00:00Z", lastUsedAt: null };
const appPasswordB = { id: 2, label: "iPad", createdAt: "2026-01-02T00:00:00Z", lastUsedAt: null };

beforeEach(() => {
  vi.clearAllMocks();
  useAppPasswordsStore.setState({ appPasswords: [] });
  useAuthStore.setState({ accessToken: "token-123" });
});

describe("fetchAppPasswords", () => {
  it("throws when there is no access token", async () => {
    useAuthStore.setState({ accessToken: null });

    await expect(useAppPasswordsStore.getState().fetchAppPasswords()).rejects.toThrow(
      "Not authenticated.",
    );
  });

  it("stores the fetched list", async () => {
    vi.mocked(appPasswordsApi.list).mockResolvedValue([appPasswordA, appPasswordB]);

    await useAppPasswordsStore.getState().fetchAppPasswords();

    expect(useAppPasswordsStore.getState().appPasswords).toEqual([appPasswordA, appPasswordB]);
    expect(appPasswordsApi.list).toHaveBeenCalledWith("token-123");
  });
});

describe("createAppPassword", () => {
  it("prepends the new app password and returns the plaintext secret", async () => {
    useAppPasswordsStore.setState({ appPasswords: [appPasswordB] });
    vi.mocked(appPasswordsApi.create).mockResolvedValue({ ...appPasswordA, secret: "the-secret" });

    const secret = await useAppPasswordsStore.getState().createAppPassword("iPhone");

    expect(secret).toBe("the-secret");
    expect(useAppPasswordsStore.getState().appPasswords).toEqual([appPasswordA, appPasswordB]);
    expect(appPasswordsApi.create).toHaveBeenCalledWith("token-123", "iPhone");
  });
});

describe("revokeAppPassword", () => {
  it("removes only the revoked app password", async () => {
    useAppPasswordsStore.setState({ appPasswords: [appPasswordA, appPasswordB] });
    vi.mocked(appPasswordsApi.revoke).mockResolvedValue(undefined);

    await useAppPasswordsStore.getState().revokeAppPassword(appPasswordA.id);

    expect(useAppPasswordsStore.getState().appPasswords).toEqual([appPasswordB]);
    expect(appPasswordsApi.revoke).toHaveBeenCalledWith("token-123", appPasswordA.id);
  });
});
