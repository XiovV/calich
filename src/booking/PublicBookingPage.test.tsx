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
    publicBookingApi: { get: vi.fn(), slots: vi.fn(), book: vi.fn() },
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
  it("marks a day with slots available, and selecting a slot only highlights it and reveals the form (nothing is submitted yet)", async () => {
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
    // Selecting a slot reveals the booking form but submits nothing on its
    // own (#326) — only "Confirm booking" calls book().
    expect(publicBookingApi.get).toHaveBeenCalledTimes(1);
    expect(publicBookingApi.book).not.toHaveBeenCalled();
    expect(screen.getByLabelText("Name")).toBeInTheDocument();
    expect(screen.getByLabelText("Email")).toBeInTheDocument();
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

describe("PublicBookingPage — the booking form (#326)", () => {
  async function selectFirstSlot(user: ReturnType<typeof userEvent.setup>, slot: Date) {
    await screen.findByText("Intro call");
    await waitFor(() => expect(publicBookingApi.slots).toHaveBeenCalled());
    const dayCell = await screen.findByRole("gridcell", { name: String(slot.getUTCDate()) });
    await user.click(within(dayCell).getByRole("button"));
    const slotButton = await screen.findByRole("button", { name: /^\d{1,2}(:\d{2})?\s?(AM|PM)?$/i });
    await user.click(slotButton);
  }

  it("confirms the booking and shows the booked time once submitted", async () => {
    vi.mocked(publicBookingApi.get).mockResolvedValue(link);
    const slot = todayAsSlot();
    vi.mocked(publicBookingApi.slots).mockResolvedValue([slot]);
    vi.mocked(publicBookingApi.book).mockResolvedValue({ start: slot, end: new Date(slot.getTime() + 30 * 60_000) });

    const user = userEvent.setup();
    renderPage();
    await selectFirstSlot(user, slot);

    await user.type(screen.getByLabelText("Name"), "Bob Visitor");
    await user.type(screen.getByLabelText("Email"), "bob@example.com");
    await user.click(screen.getByRole("button", { name: "Confirm booking" }));

    expect(publicBookingApi.book).toHaveBeenCalledWith("damir", "intro-call", slot, "Bob Visitor", "bob@example.com");
    expect(await screen.findByText("You're booked!")).toBeInTheDocument();
    expect(screen.getByText(/A confirmation has been sent to bob@example.com/)).toBeInTheDocument();
  });

  it("shows a message and lets the visitor pick again when the slot was just taken", async () => {
    vi.mocked(publicBookingApi.get).mockResolvedValue(link);
    const slot = todayAsSlot();
    vi.mocked(publicBookingApi.slots).mockResolvedValueOnce([slot]).mockResolvedValue([]);
    vi.mocked(publicBookingApi.book).mockRejectedValue(new ApiError(409, "slot_taken", "that time was just taken"));

    const user = userEvent.setup();
    renderPage();
    await selectFirstSlot(user, slot);

    await user.type(screen.getByLabelText("Name"), "Bob Visitor");
    await user.type(screen.getByLabelText("Email"), "bob@example.com");
    await user.click(screen.getByRole("button", { name: "Confirm booking" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("That time was just taken");
    // The form itself is gone once the slot is cleared — no stale selection
    // left highlighted for a slot that no longer exists.
    expect(screen.queryByLabelText("Name")).not.toBeInTheDocument();
    await waitFor(() => expect(publicBookingApi.slots).toHaveBeenCalledTimes(2));
  });
});
