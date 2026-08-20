import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";

// Same convention as AccountSection.test.tsx: authApi is mocked, authStore
// is real — this covers what RegisterPage renders off the store's state,
// not the request/response shape itself.
vi.mock("../lib/authApi", async () => {
  const actual = await vi.importActual<typeof import("../lib/authApi")>("../lib/authApi");
  return {
    ...actual,
    authApi: { ...actual.authApi, register: vi.fn() },
  };
});

const { useAuthStore } = await import("../lib/authStore");
const { RegisterPage } = await import("./RegisterPage");

function renderRegisterPage() {
  return render(
    <MemoryRouter initialEntries={["/register"]}>
      <RegisterPage />
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

// #235: with signups disabled, a direct visit to /register must state that
// registration is closed rather than presenting a fillable form the backend
// only rejects at the very end.
describe("RegisterPage — closed state (#235)", () => {
  it("renders the working form when signups are enabled", () => {
    useAuthStore.setState({ signupsEnabled: true });
    renderRegisterPage();

    expect(screen.getByLabelText("Name")).toBeInTheDocument();
    expect(screen.getByLabelText("Email")).toBeInTheDocument();
    expect(screen.getByLabelText("Password")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Create account" })).toBeInTheDocument();
  });

  it("states registration is closed instead of showing a form when signups are disabled", () => {
    useAuthStore.setState({ signupsEnabled: false });
    renderRegisterPage();

    expect(screen.getByText("Registration is closed")).toBeInTheDocument();
    expect(screen.queryByLabelText("Name")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Email")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Password")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Create account" })).not.toBeInTheDocument();
  });

  // The very first account on the instance is always allowed regardless of
  // ENABLE_SIGNUPS (ADR-0044) — the closed state must never intercept it.
  it("still renders the form during first-run bootstrap even with signups disabled", () => {
    useAuthStore.setState({ status: "needs-setup", signupsEnabled: false });
    renderRegisterPage();

    expect(screen.getByRole("button", { name: "Create account" })).toBeInTheDocument();
    expect(screen.queryByText("Registration is closed")).not.toBeInTheDocument();
  });
});
