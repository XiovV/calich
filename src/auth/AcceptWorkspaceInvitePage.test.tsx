import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";

// Same convention as RegisterPage.test.tsx: authApi is mocked, authStore is
// real — this covers what the page renders off the store's state and the
// preview response, not the request/response shape itself.
vi.mock("../lib/authApi", async () => {
  const actual = await vi.importActual<typeof import("../lib/authApi")>("../lib/authApi");
  return {
    ...actual,
    authApi: { ...actual.authApi, previewWorkspaceInvite: vi.fn(), acceptWorkspaceInvite: vi.fn() },
  };
});

const { authApi } = await import("../lib/authApi");
const { useAuthStore } = await import("../lib/authStore");
const { AcceptWorkspaceInvitePage } = await import("./AcceptWorkspaceInvitePage");

function renderAcceptPage() {
  return render(
    <MemoryRouter initialEntries={["/accept-invite?token=invite-token"]}>
      <AcceptWorkspaceInvitePage />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  useAuthStore.setState({
    status: "unauthenticated",
    user: null,
    pendingEmail: null,
    accessToken: null,
  });
});

// #255: the 8-character floor was stated in the hint text but only enforced
// server-side — a short password left "Create account and join" enabled
// until the round trip rejected it.
describe("AcceptWorkspaceInvitePage — password minimum length (#255)", () => {
  it("keeps Create account and join disabled while the password is under 8 characters", async () => {
    vi.mocked(authApi.previewWorkspaceInvite).mockResolvedValue({
      workspaceName: "Acme",
      email: "new-member@example.com",
      userExists: false,
    });
    renderAcceptPage();

    await screen.findByLabelText("Name");
    await userEvent.type(screen.getByLabelText("Name"), "Jane Smith");
    await userEvent.type(screen.getByLabelText("Password"), "short7c");
    await userEvent.type(screen.getByLabelText("Confirm password"), "short7c");
    expect(screen.getByRole("button", { name: "Create account and join" })).toBeDisabled();

    await userEvent.type(screen.getByLabelText("Password"), "8");
    await userEvent.type(screen.getByLabelText("Confirm password"), "8");
    expect(screen.getByRole("button", { name: "Create account and join" })).toBeEnabled();
  });

  // An all-whitespace password can be 8+ code points long and still fail
  // the server's isVisibleRune check (#251) — hasMinPasswordLength alone
  // wouldn't catch that, so a trim guard is needed alongside it.
  it("keeps Create account and join disabled for an 8-character whitespace-only password", async () => {
    vi.mocked(authApi.previewWorkspaceInvite).mockResolvedValue({
      workspaceName: "Acme",
      email: "new-member@example.com",
      userExists: false,
    });
    renderAcceptPage();

    await screen.findByLabelText("Name");
    await userEvent.type(screen.getByLabelText("Name"), "Jane Smith");
    await userEvent.type(screen.getByLabelText("Password"), "        ");
    await userEvent.type(screen.getByLabelText("Confirm password"), "        ");
    expect(screen.getByRole("button", { name: "Create account and join" })).toBeDisabled();
  });
});
