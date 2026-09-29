import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router";
import { ApiError } from "../lib/apiClient";

vi.mock("../lib/publicBookingApi", async () => {
  const actual = await vi.importActual<typeof import("../lib/publicBookingApi")>("../lib/publicBookingApi");
  return {
    ...actual,
    publicBookingApi: { cancel: vi.fn() },
  };
});

const { publicBookingApi } = await import("../lib/publicBookingApi");
const { CancelBookingPage } = await import("./CancelBookingPage");

function renderPage(path = "/cancel-booking?token=good-token") {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/cancel-booking" element={<CancelBookingPage />} />
      </Routes>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe("CancelBookingPage", () => {
  it("cancels the booking once the visitor confirms", async () => {
    vi.mocked(publicBookingApi.cancel).mockResolvedValue(undefined);
    const user = userEvent.setup();

    renderPage();
    await user.click(screen.getByRole("button", { name: "Cancel booking" }));

    expect(publicBookingApi.cancel).toHaveBeenCalledWith("good-token");
    expect(await screen.findByText("Booking cancelled")).toBeInTheDocument();
  });

  it("does not cancel just from opening the page", () => {
    renderPage();

    expect(screen.getByText("Cancel this booking?")).toBeInTheDocument();
    expect(publicBookingApi.cancel).not.toHaveBeenCalled();
  });

  it("shows a distinct message when the booking already started", async () => {
    vi.mocked(publicBookingApi.cancel).mockRejectedValue(new ApiError(409, "booking_already_started", "too late"));
    const user = userEvent.setup();

    renderPage();
    await user.click(screen.getByRole("button", { name: "Cancel booking" }));

    expect(await screen.findByText("This booking has already started")).toBeInTheDocument();
  });

  it("shows a distinct message for an invalid or tampered token", async () => {
    vi.mocked(publicBookingApi.cancel).mockRejectedValue(new ApiError(400, "invalid_request", "bad token"));
    const user = userEvent.setup();

    renderPage();
    await user.click(screen.getByRole("button", { name: "Cancel booking" }));

    expect(await screen.findByText("This cancel link isn't valid")).toBeInTheDocument();
  });

  it("treats cancelling twice as fine — the second visit still shows success", async () => {
    vi.mocked(publicBookingApi.cancel).mockResolvedValue(undefined);
    const user = userEvent.setup();

    renderPage();
    await user.click(screen.getByRole("button", { name: "Cancel booking" }));
    expect(await screen.findByText("Booking cancelled")).toBeInTheDocument();
  });

  it("shows the invalid-link message when the page opens with no token at all", () => {
    renderPage("/cancel-booking");

    expect(screen.getByText("This cancel link isn't valid")).toBeInTheDocument();
  });
});
