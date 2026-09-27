import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router";
import { ApiError } from "../lib/apiClient";
import type { PublicBookingLink } from "../lib/publicBookingApi";

vi.mock("../lib/publicBookingApi", async () => {
  const actual = await vi.importActual<typeof import("../lib/publicBookingApi")>("../lib/publicBookingApi");
  return {
    ...actual,
    publicBookingApi: { get: vi.fn(), slots: vi.fn() },
  };
});

const { publicBookingApi } = await import("../lib/publicBookingApi");
const { PublicBookingPage } = await import("./PublicBookingPage");

const link: PublicBookingLink = {
  hostName: "Damir",
  hostTimezone: "Europe/Sarajevo",
  title: "Intro call",
  durationMinutes: 30,
  location: "Google Meet",
  description: "A quick chat.",
  bookingHorizonDays: 60,
  paused: false,
};

function renderPage(path = "/damir/intro-call") {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/:handle/:slug" element={<PublicBookingPage />} />
      </Routes>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe("PublicBookingPage — resolving the link", () => {
  it("renders the host and offer details once the link loads", async () => {
    vi.mocked(publicBookingApi.get).mockResolvedValue(link);
    vi.mocked(publicBookingApi.slots).mockResolvedValue([]);

    renderPage();

    expect(await screen.findByText("Intro call")).toBeInTheDocument();
    expect(screen.getByText(/Damir/)).toBeInTheDocument();
    expect(screen.getByText(/30 min/)).toBeInTheDocument();
    expect(screen.getByText(/Google Meet/)).toBeInTheDocument();
    expect(screen.getByText("A quick chat.")).toBeInTheDocument();
    expect(screen.getByText(/Host timezone:/)).toHaveTextContent("Europe/Sarajevo");
    expect(publicBookingApi.get).toHaveBeenCalledWith("damir", "intro-call");
  });

  it("shows a not-found message for an unknown handle or slug (404)", async () => {
    vi.mocked(publicBookingApi.get).mockRejectedValue(new ApiError(404, "not_found", "booking link not found"));

    renderPage();

    expect(await screen.findByText("This page doesn't exist.")).toBeInTheDocument();
  });

  it("shows a generic error for anything else", async () => {
    vi.mocked(publicBookingApi.get).mockRejectedValue(new Error("network down"));

    renderPage();

    expect(await screen.findByText("Something went wrong.")).toBeInTheDocument();
  });
});

describe("PublicBookingPage — Paused", () => {
  it("says plainly that a Paused link isn't accepting bookings, without fetching slots", async () => {
    vi.mocked(publicBookingApi.get).mockResolvedValue({ ...link, paused: true });

    renderPage();

    expect(await screen.findByRole("status")).toHaveTextContent("isn't accepting bookings right now");
    expect(publicBookingApi.slots).not.toHaveBeenCalled();
  });
});

// The page's default visible month is always "today's month" — so a slot
// fixture usable across any test run has to land on today's own date rather
// than a hardcoded one. Noon UTC keeps the same calendar date in every real
// IANA zone this app or a CI runner is plausibly configured in.
function todayAsSlot(): Date {
  const now = new Date();
  return new Date(Date.UTC(now.getFullYear(), now.getMonth(), now.getDate(), 12, 0, 0));
}

describe("PublicBookingPage — month grid and slots", () => {
  it("marks a day with slots available, and selecting a slot only highlights it (nothing is submitted)", async () => {
    vi.mocked(publicBookingApi.get).mockResolvedValue(link);
    const slot = todayAsSlot();
    vi.mocked(publicBookingApi.slots).mockResolvedValue([slot]);

    renderPage();
    await screen.findByText("Intro call");
    await waitFor(() => expect(publicBookingApi.slots).toHaveBeenCalled());

    const dayCell = await screen.findByRole("gridcell", { name: String(slot.getUTCDate()) });
    const dayButton = within(dayCell).getByRole("button");
    expect(dayButton).not.toBeDisabled();

    const user = userEvent.setup();
    await user.click(dayButton);

    const slotButton = await screen.findByRole("button", { name: /^\d{1,2}(:\d{2})?\s?(AM|PM)?$/i });
    expect(slotButton).toHaveAttribute("aria-pressed", "false");

    await user.click(slotButton);
    expect(slotButton).toHaveAttribute("aria-pressed", "true");
    // Nothing was submitted — selecting a slot has no further effect (#324:
    // "Nothing can be booked yet — selecting a slot goes nowhere").
    expect(publicBookingApi.get).toHaveBeenCalledTimes(1);
  });

  it("disables a day with no slots at all", async () => {
    vi.mocked(publicBookingApi.get).mockResolvedValue(link);
    vi.mocked(publicBookingApi.slots).mockResolvedValue([]);

    renderPage();
    await screen.findByText("Intro call");
    await waitFor(() => expect(publicBookingApi.slots).toHaveBeenCalled());

    const slot = todayAsSlot();
    const dayCell = await screen.findByRole("gridcell", { name: String(slot.getUTCDate()) });
    expect(within(dayCell).getByRole("button")).toBeDisabled();
  });
});
