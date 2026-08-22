import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

// Same convention as the other component tests: the *Api modules are mocked,
// the stores are real (#234) — this covers the Settings → Account wiring to
// authStore.changePassword, not the request/response shape itself, which
// authApi.test.ts already owns.
vi.mock("../lib/authApi", async () => {
  const actual = await vi.importActual<typeof import("../lib/authApi")>("../lib/authApi");
  return {
    ...actual,
    authApi: {
      ...actual.authApi,
      changePassword: vi.fn(),
      me: vi.fn(),
      updateEmail: vi.fn(),
      updateName: vi.fn(),
    },
  };
});
vi.mock("../lib/appPasswordsApi", () => ({
  appPasswordsApi: { list: vi.fn() },
}));

const { authApi } = await import("../lib/authApi");
const { appPasswordsApi } = await import("../lib/appPasswordsApi");
const { useAuthStore } = await import("../lib/authStore");
const { AccountSection } = await import("./AccountSection");

const user = {
  id: 1,
  name: "Ada",
  mustChangePassword: false,
  email: "ada@example.com",
  emailReminderChannelAvailable: false,
  invitationRepliesConfigured: false,
  syncedDeviceRemindersEnabled: false,
  weekStart: 1,
  defaultView: "week" as const,
  timeFormat: "24h" as const,
  workingHoursStart: null,
  workingHoursEnd: null,
};

beforeEach(() => {
  vi.clearAllMocks();
  useAuthStore.setState({
    status: "authenticated",
    user,
    pendingEmail: null,
    accessToken: "token-123",
  });
});

async function fillPasswordForm(current: string, next: string, confirm: string) {
  await userEvent.type(screen.getByLabelText("Current password"), current);
  await userEvent.type(screen.getByLabelText("New password"), next);
  await userEvent.type(screen.getByLabelText("Confirm new password"), confirm);
}

const updateButton = () => screen.getByRole("button", { name: "Update password" });

describe("AccountSection — Name/Email normalization (#245)", () => {
  it("re-syncs the Name field from the server's normalized value after save", async () => {
    const updatedUser = { ...user, name: "QA Tester" };
    vi.mocked(authApi.updateName).mockResolvedValue(updatedUser);
    render(<AccountSection />);

    const nameInput = screen.getByLabelText("Name");
    await userEvent.clear(nameInput);
    await userEvent.type(nameInput, "QA Tester  ");
    const saveButton = screen.getAllByRole("button", { name: "Save" })[0];
    await userEvent.click(saveButton);

    expect(authApi.updateName).toHaveBeenCalledWith("token-123", "QA Tester");
    expect(nameInput).toHaveValue("QA Tester");
    expect(saveButton).toBeDisabled();
  });

  it("re-syncs the Email field from the server's normalized value after save", async () => {
    const updatedUser = { ...user, email: "qa.tester@calich.test" };
    vi.mocked(authApi.updateEmail).mockResolvedValue(updatedUser);
    vi.mocked(appPasswordsApi.list).mockResolvedValue([]);
    render(<AccountSection />);

    const emailInput = screen.getByLabelText("Email");
    await userEvent.clear(emailInput);
    await userEvent.type(emailInput, "QA.Tester@Calich.TEST");
    const saveButton = screen.getAllByRole("button", { name: "Save" })[1];
    await userEvent.click(saveButton);

    expect(authApi.updateEmail).toHaveBeenCalledWith("token-123", "QA.Tester@Calich.TEST");
    expect(emailInput).toHaveValue("qa.tester@calich.test");
    expect(saveButton).toBeDisabled();
  });
});

describe("AccountSection — hidden username field for password managers (#246)", () => {
  it("carries the account email on a hidden autocomplete=username input", () => {
    render(<AccountSection />);

    const usernameField = document.querySelector('input[autocomplete="username"]');
    expect(usernameField).not.toBeNull();
    expect(usernameField).toHaveValue(user.email);
    expect(usernameField).toHaveAttribute("name", "username");
    expect(usernameField).toHaveClass("sr-only");
    expect(usernameField).not.toHaveAttribute("hidden");
  });
});

describe("AccountSection — change password (#234)", () => {
  it("disables Update password until current, new and matching-confirm are all filled", async () => {
    render(<AccountSection />);
    expect(updateButton()).toBeDisabled();

    await userEvent.type(screen.getByLabelText("Current password"), "old-pw");
    expect(updateButton()).toBeDisabled();

    await userEvent.type(screen.getByLabelText("New password"), "new-pw");
    expect(updateButton()).toBeDisabled();

    await userEvent.type(screen.getByLabelText("Confirm new password"), "not-new-pw");
    expect(updateButton()).toBeDisabled();
  });

  it("changes the password and clears the form on success", async () => {
    vi.mocked(authApi.changePassword).mockResolvedValue({ accessToken: "token-456" });
    vi.mocked(authApi.me).mockResolvedValue(user);
    render(<AccountSection />);

    await fillPasswordForm("old-pw", "new-pw", "new-pw");
    await userEvent.click(updateButton());

    expect(authApi.changePassword).toHaveBeenCalledWith("token-123", "old-pw", "new-pw");
    expect(screen.getByText("Password updated.")).toBeInTheDocument();
    expect(screen.getByLabelText("Current password")).toHaveValue("");
    expect(screen.getByLabelText("New password")).toHaveValue("");
    expect(screen.getByLabelText("Confirm new password")).toHaveValue("");
  });

  it("refuses a wrong current password with a clear message and changes nothing", async () => {
    vi.mocked(authApi.changePassword).mockRejectedValue(
      new Error("current password is incorrect"),
    );
    render(<AccountSection />);

    await fillPasswordForm("wrong-pw", "new-pw", "new-pw");
    await userEvent.click(updateButton());

    expect(screen.getByText("current password is incorrect")).toBeInTheDocument();
    expect(screen.queryByText("Password updated.")).not.toBeInTheDocument();
    // The failed attempt's input stays put rather than being silently wiped.
    expect(screen.getByLabelText("Current password")).toHaveValue("wrong-pw");
    expect(useAuthStore.getState().accessToken).toBe("token-123");
  });

  it("surfaces a too-long new password with a clear message and changes nothing (#241)", async () => {
    vi.mocked(authApi.changePassword).mockRejectedValue(
      new Error("new password must be at most 72 bytes"),
    );
    render(<AccountSection />);

    const longPassword = "a".repeat(73);
    await fillPasswordForm("old-pw", longPassword, longPassword);
    await userEvent.click(updateButton());

    expect(screen.getByText("new password must be at most 72 bytes")).toBeInTheDocument();
    expect(screen.queryByText("Password updated.")).not.toBeInTheDocument();
    expect(useAuthStore.getState().accessToken).toBe("token-123");
  });

  it("flags mismatched new/confirm passwords inline", async () => {
    render(<AccountSection />);

    await fillPasswordForm("old-pw", "new-pw", "different-pw");

    expect(screen.getByText("Passwords don't match.")).toBeInTheDocument();
  });
});
