import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "./authApi";

vi.mock("./authApi", async () => {
  const actual = await vi.importActual<typeof import("./authApi")>("./authApi");
  return {
    ...actual,
    authApi: {
      login: vi.fn(),
      refresh: vi.fn(),
      logout: vi.fn(),
      me: vi.fn(),
      changePassword: vi.fn(),
      updateEmail: vi.fn(),
      updateSyncedDeviceReminders: vi.fn(),
    },
  };
});

const { authApi } = await import("./authApi");
const { useAuthStore } = await import("./authStore");

const adminUser = {
  id: 1,
  username: "admin",
  mustChangePassword: false,
  email: null,
  emailReminderChannelAvailable: false,
  syncedDeviceRemindersEnabled: false,
};

function resetStore() {
  useAuthStore.setState({
    status: "loading",
    user: null,
    pendingUsername: null,
    accessToken: null,
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  resetStore();
});

describe("bootstrap", () => {
  it("goes to unauthenticated when refresh fails", async () => {
    vi.mocked(authApi.refresh).mockRejectedValue(new ApiError(401, "unauthorized", "no session"));

    await useAuthStore.getState().bootstrap();

    expect(useAuthStore.getState().status).toBe("unauthenticated");
    expect(useAuthStore.getState().accessToken).toBeNull();
  });

  it("goes to authenticated when refresh and me both succeed", async () => {
    vi.mocked(authApi.refresh).mockResolvedValue({ accessToken: "token-123" });
    vi.mocked(authApi.me).mockResolvedValue(adminUser);

    await useAuthStore.getState().bootstrap();

    const state = useAuthStore.getState();
    expect(state.status).toBe("authenticated");
    expect(state.user).toEqual(adminUser);
    expect(state.accessToken).toBe("token-123");
  });

  it("goes to must-change-password when me is blocked with that code", async () => {
    vi.mocked(authApi.refresh).mockResolvedValue({ accessToken: "token-123" });
    vi.mocked(authApi.me).mockRejectedValue(
      new ApiError(403, "password_change_required", "password must be changed"),
    );

    await useAuthStore.getState().bootstrap();

    const state = useAuthStore.getState();
    expect(state.status).toBe("must-change-password");
    expect(state.accessToken).toBe("token-123");
    expect(state.user).toBeNull();
  });
});

describe("login", () => {
  it("fetches the full user and goes to authenticated when a password change isn't required", async () => {
    vi.mocked(authApi.login).mockResolvedValue({ accessToken: "token-123", mustChangePassword: false });
    vi.mocked(authApi.me).mockResolvedValue(adminUser);

    await useAuthStore.getState().login("admin", "admin");

    const state = useAuthStore.getState();
    expect(state.status).toBe("authenticated");
    expect(state.user?.username).toBe("admin");
  });

  it("goes to must-change-password without calling me when required", async () => {
    vi.mocked(authApi.login).mockResolvedValue({ accessToken: "token-123", mustChangePassword: true });

    await useAuthStore.getState().login("admin", "admin");

    const state = useAuthStore.getState();
    expect(state.status).toBe("must-change-password");
    expect(state.pendingUsername).toBe("admin");
    expect(authApi.me).not.toHaveBeenCalled();
  });

  it("propagates login errors without changing state", async () => {
    vi.mocked(authApi.login).mockRejectedValue(new ApiError(401, "invalid_credentials", "nope"));

    await expect(useAuthStore.getState().login("admin", "wrong")).rejects.toMatchObject({
      code: "invalid_credentials",
    });
    expect(useAuthStore.getState().status).toBe("loading");
  });
});

describe("logout", () => {
  it("clears local state even if the backend call fails", async () => {
    useAuthStore.setState({
      status: "authenticated",
      user: adminUser,
      pendingUsername: null,
      accessToken: "token-123",
    });
    vi.mocked(authApi.logout).mockRejectedValue(new Error("network error"));

    await useAuthStore.getState().logout();

    const state = useAuthStore.getState();
    expect(state.status).toBe("unauthenticated");
    expect(state.user).toBeNull();
    expect(state.accessToken).toBeNull();
  });
});

describe("changePassword", () => {
  it("throws when there is no access token", async () => {
    await expect(useAuthStore.getState().changePassword("old", "new")).rejects.toThrow(
      "Not authenticated.",
    );
  });

  it("fetches the fresh user and goes to authenticated on success", async () => {
    useAuthStore.setState({
      status: "must-change-password",
      user: null,
      pendingUsername: "admin",
      accessToken: "token-123",
    });
    vi.mocked(authApi.changePassword).mockResolvedValue(undefined);
    vi.mocked(authApi.me).mockResolvedValue(adminUser);

    await useAuthStore.getState().changePassword("old-pw", "new-pw");

    const state = useAuthStore.getState();
    expect(state.status).toBe("authenticated");
    expect(state.user?.mustChangePassword).toBe(false);
    expect(state.pendingUsername).toBeNull();
  });
});

describe("updateEmail", () => {
  it("throws when there is no access token", async () => {
    await expect(useAuthStore.getState().updateEmail("admin@example.com")).rejects.toThrow(
      "Not authenticated.",
    );
  });

  it("stores the fresh user returned by the API", async () => {
    useAuthStore.setState({
      status: "authenticated",
      user: adminUser,
      pendingUsername: null,
      accessToken: "token-123",
    });
    const updatedUser = { ...adminUser, email: "admin@example.com", emailReminderChannelAvailable: true };
    vi.mocked(authApi.updateEmail).mockResolvedValue(updatedUser);

    await useAuthStore.getState().updateEmail("admin@example.com");

    expect(useAuthStore.getState().user).toEqual(updatedUser);
    expect(authApi.updateEmail).toHaveBeenCalledWith("token-123", "admin@example.com");
  });
});

describe("updateSyncedDeviceReminders", () => {
  it("throws when there is no access token", async () => {
    await expect(useAuthStore.getState().updateSyncedDeviceReminders(true)).rejects.toThrow(
      "Not authenticated.",
    );
  });

  it("stores the fresh user returned by the API", async () => {
    useAuthStore.setState({
      status: "authenticated",
      user: adminUser,
      pendingUsername: null,
      accessToken: "token-123",
    });
    const updatedUser = { ...adminUser, syncedDeviceRemindersEnabled: true };
    vi.mocked(authApi.updateSyncedDeviceReminders).mockResolvedValue(updatedUser);

    await useAuthStore.getState().updateSyncedDeviceReminders(true);

    expect(useAuthStore.getState().user).toEqual(updatedUser);
    expect(authApi.updateSyncedDeviceReminders).toHaveBeenCalledWith("token-123", true);
  });
});
