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

describe("AccountSection — re-syncs from a save that touched a different field (#245 reopened)", () => {
  it("re-syncs the Email field, and re-disables its Save, after a Name-only save changes the store's email", async () => {
    // Simulates the two-tab repro without needing two tabs: the store's
    // `user` can change for a reason that has nothing to do with the Email
    // field's own save (e.g. another tab's Email change, reflected back
    // here the next time *any* save returns the canonical user).
    const updatedUser = { ...user, name: "New Name", email: "new-address@calich.test" };
    vi.mocked(authApi.updateName).mockResolvedValue(updatedUser);
    render(<AccountSection />);

    const nameInput = screen.getByLabelText("Name");
    const emailInput = screen.getByLabelText("Email");
    const nameSaveButton = screen.getAllByRole("button", { name: "Save" })[0];
    const emailSaveButton = screen.getAllByRole("button", { name: "Save" })[1];

    expect(emailInput).toHaveValue(user.email);
    expect(emailSaveButton).toBeDisabled();

    await userEvent.clear(nameInput);
    await userEvent.type(nameInput, "New Name");
    await userEvent.click(nameSaveButton);

    expect(authApi.updateName).toHaveBeenCalledWith("token-123", "New Name");
    // The Email field never received user input — it must reflect the
    // store's current email, not the stale value captured at mount.
    expect(emailInput).toHaveValue("new-address@calich.test");
    // And Save must not have silently re-enabled itself off that resync.
    expect(emailSaveButton).toBeDisabled();
  });

  it("does not clobber an in-progress, unsaved Email edit when a Name save resolves", async () => {
    const updatedUser = { ...user, name: "New Name" };
    vi.mocked(authApi.updateName).mockResolvedValue(updatedUser);
    render(<AccountSection />);

    const nameInput = screen.getByLabelText("Name");
    const emailInput = screen.getByLabelText("Email");
    const nameSaveButton = screen.getAllByRole("button", { name: "Save" })[0];

    await userEvent.clear(emailInput);
    await userEvent.type(emailInput, "still-typing@calich.test");

    await userEvent.clear(nameInput);
    await userEvent.type(nameInput, "New Name");
    await userEvent.click(nameSaveButton);

    expect(authApi.updateName).toHaveBeenCalledWith("token-123", "New Name");
    expect(emailInput).toHaveValue("still-typing@calich.test");
  });

  it("resumes tracking the store's Name after an edit is manually reverted to the original value, unsaved", async () => {
    // A "dirty" flag latched on keystroke and cleared only by *this* field's
    // own save gets stuck forever if the edit is reverted by hand instead of
    // saved: the revert never runs that clearing code, so a later external
    // change (another field's save, another tab) would go unreflected here
    // for the rest of the session.
    const updatedUser = { ...user, email: "new-address@calich.test", name: "Externally Changed" };
    vi.mocked(authApi.updateEmail).mockResolvedValue(updatedUser);
    vi.mocked(appPasswordsApi.list).mockResolvedValue([]);
    render(<AccountSection />);

    const nameInput = screen.getByLabelText("Name");
    const emailInput = screen.getByLabelText("Email");
    const emailSaveButton = screen.getAllByRole("button", { name: "Save" })[1];

    await userEvent.type(nameInput, "x");
    await userEvent.type(nameInput, "{backspace}");
    expect(nameInput).toHaveValue(user.name);

    // The Name field's own save is never touched from here on — only Email
    // is saved. If reverting above left Name's "dirty" flag stuck true, this
    // externally-driven name change (e.g. from another tab) would never
    // reach it.
    await userEvent.clear(emailInput);
    await userEvent.type(emailInput, "new-address@calich.test");
    await userEvent.click(emailSaveButton);

    expect(authApi.updateEmail).toHaveBeenCalledWith("token-123", "new-address@calich.test");
    expect(nameInput).toHaveValue("Externally Changed");
  });

  it("clears a stale 'Saved.' message when a field re-syncs passively rather than from its own save", async () => {
    const emailSavedUser = { ...user, email: "own-save@calich.test" };
    vi.mocked(authApi.updateEmail).mockResolvedValue(emailSavedUser);
    vi.mocked(appPasswordsApi.list).mockResolvedValue([]);
    render(<AccountSection />);

    const emailInput = screen.getByLabelText("Email");
    const emailSaveButton = screen.getAllByRole("button", { name: "Save" })[1];

    await userEvent.clear(emailInput);
    await userEvent.type(emailInput, "own-save@calich.test");
    await userEvent.click(emailSaveButton);
    expect(screen.getByText("Saved.")).toBeInTheDocument();

    const nameOnlyUser = { ...emailSavedUser, name: "New Name", email: "someone-else@calich.test" };
    vi.mocked(authApi.updateName).mockResolvedValue(nameOnlyUser);
    const nameInput = screen.getByLabelText("Name");
    const nameSaveButton = screen.getAllByRole("button", { name: "Save" })[0];
    await userEvent.clear(nameInput);
    await userEvent.type(nameInput, "New Name");
    await userEvent.click(nameSaveButton);

    expect(emailInput).toHaveValue("someone-else@calich.test");
    // Exactly one "Saved." now: Name's own, freshly earned by the save that
    // just ran. Email's earlier one must not still be showing — that would
    // misattribute a value this tab never actually submitted.
    expect(screen.getAllByText("Saved.")).toHaveLength(1);
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

  // #250: sr-only keeps it out of the visual layout but not the tab order or
  // accessibility tree, so it was a stop between Email and Current password
  // with no accessible name. It must stay reachable by password managers
  // (DOM presence + autocomplete) without being a keyboard/screen-reader stop.
  it("is unfocusable and hidden from the accessibility tree (#250)", () => {
    render(<AccountSection />);

    const usernameField = document.querySelector('input[autocomplete="username"]');
    expect(usernameField).toHaveAttribute("tabIndex", "-1");
    expect(usernameField).toHaveAttribute("aria-hidden", "true");
  });
});

describe("AccountSection — change password (#234)", () => {
  it("disables Update password until current, new and matching-confirm are all filled", async () => {
    render(<AccountSection />);
    expect(updateButton()).toBeDisabled();

    await userEvent.type(screen.getByLabelText("Current password"), "old-pw");
    expect(updateButton()).toBeDisabled();

    await userEvent.type(screen.getByLabelText("New password"), "new-password");
    expect(updateButton()).toBeDisabled();

    await userEvent.type(screen.getByLabelText("Confirm new password"), "not-new-password");
    expect(updateButton()).toBeDisabled();
  });

  // #255: the 8-character floor was stated in the hint text but only
  // enforced server-side — a short-but-otherwise-valid new password left
  // Update password enabled until the round trip rejected it.
  it("keeps Update password disabled while the new password is under 8 characters", async () => {
    render(<AccountSection />);

    await userEvent.type(screen.getByLabelText("Current password"), "old-pw");
    await userEvent.type(screen.getByLabelText("New password"), "short7c");
    await userEvent.type(screen.getByLabelText("Confirm new password"), "short7c");
    expect(updateButton()).toBeDisabled();

    await userEvent.type(screen.getByLabelText("New password"), "8");
    await userEvent.type(screen.getByLabelText("Confirm new password"), "8");
    expect(updateButton()).toBeEnabled();
  });

  it("changes the password and clears the form on success", async () => {
    vi.mocked(authApi.changePassword).mockResolvedValue({ accessToken: "token-456" });
    vi.mocked(authApi.me).mockResolvedValue(user);
    render(<AccountSection />);

    await fillPasswordForm("old-pw", "new-password", "new-password");
    await userEvent.click(updateButton());

    expect(authApi.changePassword).toHaveBeenCalledWith("token-123", "old-pw", "new-password");
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

    await fillPasswordForm("wrong-pw", "new-password", "new-password");
    await userEvent.click(updateButton());

    expect(screen.getByText("current password is incorrect")).toBeInTheDocument();
    expect(screen.queryByText("Password updated.")).not.toBeInTheDocument();
    // The failed attempt's input stays put rather than being silently wiped.
    expect(screen.getByLabelText("Current password")).toHaveValue("wrong-pw");
    expect(useAuthStore.getState().accessToken).toBe("token-123");
  });

  it("surfaces a too-long new password with a clear message and changes nothing (#241)", async () => {
    vi.mocked(authApi.changePassword).mockRejectedValue(
      new Error("new password is too long, please choose a shorter one"),
    );
    render(<AccountSection />);

    const longPassword = "a".repeat(73);
    await fillPasswordForm("old-pw", longPassword, longPassword);
    await userEvent.click(updateButton());

    expect(
      screen.getByText("new password is too long, please choose a shorter one"),
    ).toBeInTheDocument();
    expect(screen.queryByText("Password updated.")).not.toBeInTheDocument();
    expect(useAuthStore.getState().accessToken).toBe("token-123");
  });

  it("flags mismatched new/confirm passwords inline", async () => {
    render(<AccountSection />);

    await fillPasswordForm("old-pw", "new-password", "different-password");

    expect(screen.getByText("Passwords don't match.")).toBeInTheDocument();
  });
});
