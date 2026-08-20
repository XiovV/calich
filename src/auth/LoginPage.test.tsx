import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";

// Same convention as AccountSection.test.tsx: authApi is mocked, authStore
// is real — this covers what LoginPage renders off the store's state, not
// the request/response shape itself.
vi.mock("../lib/authApi", async () => {
  const actual = await vi.importActual<typeof import("../lib/authApi")>("../lib/authApi");
  return {
    ...actual,
    authApi: { ...actual.authApi, login: vi.fn() },
  };
});

const { useAuthStore } = await import("../lib/authStore");
const { LoginPage } = await import("./LoginPage");

function renderLoginPage() {
  return render(
    <MemoryRouter initialEntries={["/login"]}>
      <LoginPage />
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

// #235: an instance with ENABLE_SIGNUPS off shouldn't invite a visitor to
// register, since the backend will only reject them once they've already
// filled the form in.
describe("LoginPage — registration link (#235)", () => {
  it("shows the Create one link when signups are enabled", () => {
    useAuthStore.setState({ signupsEnabled: true });
    renderLoginPage();

    expect(screen.getByRole("link", { name: "Create one" })).toBeInTheDocument();
  });

  it("hides the Create one link when signups are disabled", () => {
    useAuthStore.setState({ signupsEnabled: false });
    renderLoginPage();

    expect(screen.queryByRole("link", { name: "Create one" })).not.toBeInTheDocument();
  });
});
