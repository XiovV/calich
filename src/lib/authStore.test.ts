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
      updateName: vi.fn(),
      setupStatus: vi.fn(),
      updateSyncedDeviceReminders: vi.fn(),
      updatePreferences: vi.fn(),
    },
  };
});

// authStore registers its session refresher with apiClient at module init
// (#124) — captured here so the "session refresher" tests below can invoke
// it directly, the way apiClient's authedFetch would on a 401.
let registeredRefresher: (() => Promise<string>) | undefined;
vi.mock("./apiClient", async () => {
  const actual = await vi.importActual<typeof import("./apiClient")>("./apiClient");
  return {
    ...actual,
    setSessionRefresher: vi.fn((refresher: () => Promise<string>) => {
      registeredRefresher = refresher;
    }),
  };
});

const { authApi } = await import("./authApi");
const { useAuthStore } = await import("./authStore");
const { useShellStore } = await import("./shellStore");

const adminUser = {
  id: 1,
  name: "admin",
  mustChangePassword: false,
  email: "admin@example.com",
  emailReminderChannelAvailable: false,
  invitationRepliesConfigured: false,
  syncedDeviceRemindersEnabled: false,
  weekStart: 1,
  defaultView: "week" as const,
  timeFormat: "24h" as const,
  workingHoursStart: null,
  workingHoursEnd: null,
};

function resetStore() {
  useAuthStore.setState({
    status: "loading",
    user: null,
    pendingEmail: null,
    accessToken: null,
    signupsEnabled: true,
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  resetStore();
  useShellStore.setState({ activeView: "week" });
  // bootstrap fetches setup-status unconditionally now (#235), so give every
  // test a default it can ignore; tests that care override it themselves.
  vi.mocked(authApi.setupStatus).mockResolvedValue({ hasAccounts: true, signupsEnabled: true });
});

describe("bootstrap", () => {
  it("goes to unauthenticated when refresh fails but the instance has accounts", async () => {
    vi.mocked(authApi.refresh).mockRejectedValue(new ApiError(401, "unauthorized", "no session"));
    vi.mocked(authApi.setupStatus).mockResolvedValue({ hasAccounts: true, signupsEnabled: true });

    await useAuthStore.getState().bootstrap();

    expect(useAuthStore.getState().status).toBe("unauthenticated");
    expect(useAuthStore.getState().accessToken).toBeNull();
  });

  // #169, ADR-0047: a fresh instance with zero accounts sends an
  // unauthenticated visitor to needs-setup instead of unauthenticated, so
  // ProtectedRoute/LoginPage redirect to Register instead of Sign-in.
  it("goes to needs-setup when refresh fails and the instance has no accounts", async () => {
    vi.mocked(authApi.refresh).mockRejectedValue(new ApiError(401, "unauthorized", "no session"));
    vi.mocked(authApi.setupStatus).mockResolvedValue({ hasAccounts: false, signupsEnabled: false });

    await useAuthStore.getState().bootstrap();

    expect(useAuthStore.getState().status).toBe("needs-setup");
    expect(useAuthStore.getState().accessToken).toBeNull();
  });

  // #235: signupsEnabled is fetched alongside hasAccounts so Sign-in/Register
  // know whether to offer registration once an account already exists.
  it("stores signupsEnabled from setup-status when refresh fails", async () => {
    vi.mocked(authApi.refresh).mockRejectedValue(new ApiError(401, "unauthorized", "no session"));
    vi.mocked(authApi.setupStatus).mockResolvedValue({ hasAccounts: true, signupsEnabled: false });

    await useAuthStore.getState().bootstrap();

    expect(useAuthStore.getState().signupsEnabled).toBe(false);
  });

  // signupsEnabled must still be correct for a caller who reaches
  // "unauthenticated" later via logout — which never re-runs bootstrap — so
  // it's fetched even down the refresh-succeeds path, not only on failure.
  it("stores signupsEnabled from setup-status even when refresh succeeds", async () => {
    vi.mocked(authApi.refresh).mockResolvedValue({ accessToken: "token-123" });
    vi.mocked(authApi.me).mockResolvedValue(adminUser);
    vi.mocked(authApi.setupStatus).mockResolvedValue({ hasAccounts: true, signupsEnabled: false });

    await useAuthStore.getState().bootstrap();

    expect(useAuthStore.getState().signupsEnabled).toBe(false);
  });

  it("falls back to unauthenticated if setup-status itself fails", async () => {
    vi.mocked(authApi.refresh).mockRejectedValue(new ApiError(401, "unauthorized", "no session"));
    vi.mocked(authApi.setupStatus).mockRejectedValue(new Error("network error"));

    await useAuthStore.getState().bootstrap();

    expect(useAuthStore.getState().status).toBe("unauthenticated");
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
    vi.mocked(authApi.login).mockResolvedValue({ accessToken: "token-123", mustChangePassword: false, isDisabled: false });
    vi.mocked(authApi.me).mockResolvedValue(adminUser);

    await useAuthStore.getState().login("admin", "admin");

    const state = useAuthStore.getState();
    expect(state.status).toBe("authenticated");
    expect(state.user?.name).toBe("admin");
  });

  it("goes to must-change-password without calling me when required", async () => {
    vi.mocked(authApi.login).mockResolvedValue({ accessToken: "token-123", mustChangePassword: true, isDisabled: false });

    await useAuthStore.getState().login("admin", "admin");

    const state = useAuthStore.getState();
    expect(state.status).toBe("must-change-password");
    expect(state.pendingEmail).toBe("admin");
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
      pendingEmail: null,
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

  it("fetches the fresh user and goes to authenticated on success, using the reissued access token", async () => {
    useAuthStore.setState({
      status: "must-change-password",
      user: null,
      pendingEmail: "admin",
      accessToken: "token-123",
    });
    vi.mocked(authApi.changePassword).mockResolvedValue({ accessToken: "token-456" });
    vi.mocked(authApi.me).mockResolvedValue(adminUser);

    await useAuthStore.getState().changePassword("old-pw", "new-pw");

    expect(authApi.me).toHaveBeenCalledWith("token-456");
    const state = useAuthStore.getState();
    expect(state.status).toBe("authenticated");
    expect(state.user?.mustChangePassword).toBe(false);
    expect(state.pendingEmail).toBeNull();
    expect(state.accessToken).toBe("token-456");
  });

  it("keeps the reissued access token even if the follow-up me() fails (#234)", async () => {
    useAuthStore.setState({
      status: "authenticated",
      user: adminUser,
      pendingEmail: null,
      accessToken: "token-123",
    });
    vi.mocked(authApi.changePassword).mockResolvedValue({ accessToken: "token-456" });
    vi.mocked(authApi.me).mockRejectedValue(new Error("network blip"));

    await expect(
      useAuthStore.getState().changePassword("old-pw", "new-pw"),
    ).rejects.toThrow("network blip");

    // The old token is already dead server-side the moment changePassword
    // resolved — the store must not be left pointing at it.
    expect(useAuthStore.getState().accessToken).toBe("token-456");
  });

  it("seeds the shell's Active view, completing the login that was blocked on the password change", async () => {
    useAuthStore.setState({
      status: "must-change-password",
      user: null,
      pendingEmail: "admin",
      accessToken: "token-123",
    });
    useShellStore.setState({ activeView: "week" });
    vi.mocked(authApi.changePassword).mockResolvedValue({ accessToken: "token-456" });
    vi.mocked(authApi.me).mockResolvedValue({ ...adminUser, defaultView: "day" });

    await useAuthStore.getState().changePassword("old-pw", "new-pw");

    expect(useShellStore.getState().activeView).toBe("day");
  });

  it("leaves the shell's Active view alone when already authenticated (#234)", async () => {
    useAuthStore.setState({
      status: "authenticated",
      user: adminUser,
      pendingEmail: null,
      accessToken: "token-123",
    });
    useShellStore.setState({ activeView: "day" });
    vi.mocked(authApi.changePassword).mockResolvedValue({ accessToken: "token-456" });
    vi.mocked(authApi.me).mockResolvedValue({ ...adminUser, defaultView: "week" });

    await useAuthStore.getState().changePassword("old-pw", "new-pw");

    expect(useShellStore.getState().activeView).toBe("day");
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
      pendingEmail: null,
      accessToken: "token-123",
    });
    const updatedUser = { ...adminUser, email: "admin@example.com", emailReminderChannelAvailable: true };
    vi.mocked(authApi.updateEmail).mockResolvedValue(updatedUser);

    await useAuthStore.getState().updateEmail("admin@example.com");

    expect(useAuthStore.getState().user).toEqual(updatedUser);
    expect(authApi.updateEmail).toHaveBeenCalledWith("token-123", "admin@example.com");
  });
});

describe("updateName", () => {
  it("throws when there is no access token", async () => {
    await expect(useAuthStore.getState().updateName("newname")).rejects.toThrow(
      "Not authenticated.",
    );
  });

  it("stores the fresh user returned by the API", async () => {
    useAuthStore.setState({
      status: "authenticated",
      user: adminUser,
      pendingEmail: null,
      accessToken: "token-123",
    });
    const updatedUser = { ...adminUser, name: "newname" };
    vi.mocked(authApi.updateName).mockResolvedValue(updatedUser);

    await useAuthStore.getState().updateName("newname");

    expect(useAuthStore.getState().user).toEqual(updatedUser);
    expect(authApi.updateName).toHaveBeenCalledWith("token-123", "newname");
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
      pendingEmail: null,
      accessToken: "token-123",
    });
    const updatedUser = { ...adminUser, syncedDeviceRemindersEnabled: true };
    vi.mocked(authApi.updateSyncedDeviceReminders).mockResolvedValue(updatedUser);

    await useAuthStore.getState().updateSyncedDeviceReminders(true);

    expect(useAuthStore.getState().user).toEqual(updatedUser);
    expect(authApi.updateSyncedDeviceReminders).toHaveBeenCalledWith("token-123", true);
  });
});

describe("updateWeekStart", () => {
  it("throws when there is no access token", async () => {
    await expect(useAuthStore.getState().updateWeekStart(0)).rejects.toThrow("Not authenticated.");
  });

  it("stores the fresh user returned by the API", async () => {
    useAuthStore.setState({
      status: "authenticated",
      user: adminUser,
      pendingEmail: null,
      accessToken: "token-123",
    });
    const updatedUser = { ...adminUser, weekStart: 0 };
    vi.mocked(authApi.updatePreferences).mockResolvedValue(updatedUser);

    await useAuthStore.getState().updateWeekStart(0);

    expect(useAuthStore.getState().user).toEqual(updatedUser);
    expect(authApi.updatePreferences).toHaveBeenCalledWith("token-123", { weekStart: 0 });
  });
});

describe("updateDefaultView", () => {
  it("throws when there is no access token", async () => {
    await expect(useAuthStore.getState().updateDefaultView("month")).rejects.toThrow(
      "Not authenticated.",
    );
  });

  it("stores the fresh user returned by the API without touching the shell's Active view", async () => {
    useAuthStore.setState({
      status: "authenticated",
      user: adminUser,
      pendingEmail: null,
      accessToken: "token-123",
    });
    useShellStore.setState({ activeView: "day" });
    const updatedUser = { ...adminUser, defaultView: "month" as const };
    vi.mocked(authApi.updatePreferences).mockResolvedValue(updatedUser);

    await useAuthStore.getState().updateDefaultView("month");

    expect(useAuthStore.getState().user).toEqual(updatedUser);
    expect(authApi.updatePreferences).toHaveBeenCalledWith("token-123", { defaultView: "month" });
    // Settings only updates the Preference — it is not "last-used wins", so
    // the caller's current Active view stays exactly where they left it.
    expect(useShellStore.getState().activeView).toBe("day");
  });
});

describe("updateTimeFormat", () => {
  it("throws when there is no access token", async () => {
    await expect(useAuthStore.getState().updateTimeFormat("12h")).rejects.toThrow(
      "Not authenticated.",
    );
  });

  it("stores the fresh user returned by the API", async () => {
    useAuthStore.setState({
      status: "authenticated",
      user: adminUser,
      pendingEmail: null,
      accessToken: "token-123",
    });
    const updatedUser = { ...adminUser, timeFormat: "12h" as const };
    vi.mocked(authApi.updatePreferences).mockResolvedValue(updatedUser);

    await useAuthStore.getState().updateTimeFormat("12h");

    expect(useAuthStore.getState().user).toEqual(updatedUser);
    expect(authApi.updatePreferences).toHaveBeenCalledWith("token-123", { timeFormat: "12h" });
  });
});

describe("updateWorkingHours", () => {
  it("throws when there is no access token", async () => {
    await expect(useAuthStore.getState().updateWorkingHours({ start: 9, end: 17 })).rejects.toThrow(
      "Not authenticated.",
    );
  });

  it("stores the fresh user returned by the API", async () => {
    useAuthStore.setState({
      status: "authenticated",
      user: adminUser,
      pendingEmail: null,
      accessToken: "token-123",
    });
    const updatedUser = { ...adminUser, workingHoursStart: 9, workingHoursEnd: 17 };
    vi.mocked(authApi.updatePreferences).mockResolvedValue(updatedUser);

    await useAuthStore.getState().updateWorkingHours({ start: 9, end: 17 });

    expect(useAuthStore.getState().user).toEqual(updatedUser);
    expect(authApi.updatePreferences).toHaveBeenCalledWith("token-123", {
      workingHours: { start: 9, end: 17 },
    });
  });

  it("clears working hours by passing null", async () => {
    useAuthStore.setState({
      status: "authenticated",
      user: { ...adminUser, workingHoursStart: 9, workingHoursEnd: 17 },
      pendingEmail: null,
      accessToken: "token-123",
    });
    const updatedUser = { ...adminUser, workingHoursStart: null, workingHoursEnd: null };
    vi.mocked(authApi.updatePreferences).mockResolvedValue(updatedUser);

    await useAuthStore.getState().updateWorkingHours(null);

    expect(useAuthStore.getState().user).toEqual(updatedUser);
    expect(authApi.updatePreferences).toHaveBeenCalledWith("token-123", { workingHours: null });
  });
});

describe("shell seeding", () => {
  it("bootstrap seeds the shell's Active view from the resolved user's Default view", async () => {
    vi.mocked(authApi.refresh).mockResolvedValue({ accessToken: "token-123" });
    vi.mocked(authApi.me).mockResolvedValue({ ...adminUser, defaultView: "month" });

    await useAuthStore.getState().bootstrap();

    expect(useShellStore.getState().activeView).toBe("month");
  });

  it("login seeds the shell's Active view from the resolved user's Default view", async () => {
    vi.mocked(authApi.login).mockResolvedValue({ accessToken: "token-123", mustChangePassword: false, isDisabled: false });
    vi.mocked(authApi.me).mockResolvedValue({ ...adminUser, defaultView: "year" });

    await useAuthStore.getState().login("admin", "admin");

    expect(useShellStore.getState().activeView).toBe("year");
  });

  it("switching the Active view mid-session does not PATCH the Default view preference", () => {
    useShellStore.getState().setActiveView("month");

    expect(useShellStore.getState().activeView).toBe("month");
    expect(authApi.updatePreferences).not.toHaveBeenCalled();
  });
});

describe("session refresher", () => {
  it("is registered with apiClient at module init", () => {
    expect(registeredRefresher).toBeInstanceOf(Function);
  });

  it("writes the new access token into the store and returns it, keeping the caller authenticated", async () => {
    useAuthStore.setState({
      status: "authenticated",
      user: adminUser,
      pendingEmail: null,
      accessToken: "stale-token",
    });
    vi.mocked(authApi.refresh).mockResolvedValue({ accessToken: "fresh-token" });

    const result = await registeredRefresher!();

    expect(result).toBe("fresh-token");
    const state = useAuthStore.getState();
    expect(state.accessToken).toBe("fresh-token");
    expect(state.status).toBe("authenticated");
    expect(state.user).toEqual(adminUser);
  });

  it("sets the store to unauthenticated and rethrows when the refresh fails", async () => {
    useAuthStore.setState({
      status: "authenticated",
      user: adminUser,
      pendingEmail: null,
      accessToken: "stale-token",
    });
    vi.mocked(authApi.refresh).mockRejectedValue(new ApiError(401, "unauthorized", "no session"));

    await expect(registeredRefresher!()).rejects.toMatchObject({ code: "unauthorized" });

    const state = useAuthStore.getState();
    expect(state.status).toBe("unauthenticated");
    expect(state.user).toBeNull();
    expect(state.accessToken).toBeNull();
  });
});
