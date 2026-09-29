import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

// Same convention as AvailabilitySchedulesSection.test.tsx: the *Api module
// is mocked, the stores are real (#322, ADR-0084, ADR-0087) — this covers
// the sidebar section's wiring, not the request/response shape itself.
vi.mock("../../lib/bookingLinksApi", async () => {
  const actual = await vi.importActual<typeof import("../../lib/bookingLinksApi")>("../../lib/bookingLinksApi");
  return {
    ...actual,
    bookingLinksApi: {
      list: vi.fn(),
      create: vi.fn(),
      update: vi.fn(),
      remove: vi.fn(),
      duplicate: vi.fn(),
      addConflictCalendar: vi.fn(),
      removeConflictCalendar: vi.fn(),
    },
  };
});
vi.mock("../../lib/authApi", async () => {
  const actual = await vi.importActual<typeof import("../../lib/authApi")>("../../lib/authApi");
  return { ...actual, authApi: { ...actual.authApi, getHandleSuggestion: vi.fn(), me: vi.fn() } };
});
vi.mock("../../lib/availabilitySchedulesApi", async () => {
  const actual = await vi.importActual<typeof import("../../lib/availabilitySchedulesApi")>(
    "../../lib/availabilitySchedulesApi",
  );
  return { ...actual, availabilitySchedulesApi: { ...actual.availabilitySchedulesApi, list: vi.fn() } };
});

const { bookingLinksApi } = await import("../../lib/bookingLinksApi");
const { authApi } = await import("../../lib/authApi");
const { availabilitySchedulesApi } = await import("../../lib/availabilitySchedulesApi");
const { useAuthStore } = await import("../../lib/authStore");
const { useWorkspacesStore } = await import("../../lib/workspacesStore");
const { useCalendarsStore } = await import("../../lib/calendarsStore");
const { useAvailabilitySchedulesStore } = await import("../../lib/availabilitySchedulesStore");
const { useBookingLinksStore } = await import("../../lib/bookingLinksStore");
const { BookingLinksSection } = await import("./BookingLinksSection");

const user = {
  id: 1,
  name: "Ada",
  mustChangePassword: false,
  email: "ada@example.com",
  emailReminderChannelAvailable: false,
  googleProviderAvailable: false,
  invitationRepliesConfigured: false,
  syncedDeviceRemindersEnabled: false,
  weekStart: 1,
  defaultView: "week" as const,
  timeFormat: "24h" as const,
  workingHoursStart: null,
  workingHoursEnd: null,
  handle: "ada",
};

const workCalendar = { id: "cal-1", name: "Work", color: "#12809CFF", access: "owner" as const };

const introCall = {
  id: 1,
  title: "Intro call",
  slug: "intro-call",
  durationMinutes: 30,
  visibility: "public" as const,
  availabilityScheduleId: 1,
  bookIntoCalendarId: "cal-1",
  location: "",
  description: "",
  minimumNoticeMinutes: 240,
  bookingHorizonDays: 60,
  tasksInConflictSet: true,
  conflictCalendarIds: ["cal-1"],
};

beforeEach(() => {
  vi.clearAllMocks();
  useAuthStore.setState({ status: "authenticated", user, accessToken: "token-123" });
  useWorkspacesStore.setState({ activeWorkspaceId: 7 });
  useCalendarsStore.setState({ calendars: [workCalendar] });
  useAvailabilitySchedulesStore.setState({ schedules: [{ id: 1, name: "Default", tzid: "Etc/UTC", ranges: [] }] });
  useBookingLinksStore.setState({ bookingLinks: [] });
  vi.mocked(bookingLinksApi.list).mockResolvedValue([]);
  vi.mocked(authApi.getHandleSuggestion).mockResolvedValue("ada-2");
  vi.mocked(availabilitySchedulesApi.list).mockResolvedValue([]);
  Object.assign(navigator, { clipboard: { writeText: vi.fn().mockResolvedValue(undefined) } });
});

describe("BookingLinksSection — listing", () => {
  it("shows a placeholder when there are no links", async () => {
    render(<BookingLinksSection />);

    expect(await screen.findByText("No booking links yet.")).toBeInTheDocument();
  });

  it("lists a link with its duration and visibility", async () => {
    vi.mocked(bookingLinksApi.list).mockResolvedValue([introCall]);
    render(<BookingLinksSection />);

    expect(await screen.findByText("Intro call")).toBeInTheDocument();
    expect(screen.getByText("30m · Public")).toBeInTheDocument();
  });
});

describe("BookingLinksSection — creating", () => {
  it("creates a new link from the modal", async () => {
    render(<BookingLinksSection />);
    await waitFor(() => expect(bookingLinksApi.list).toHaveBeenCalled());

    vi.mocked(bookingLinksApi.create).mockResolvedValue({ ...introCall, id: 2, title: "Deep dive" });

    await userEvent.click(await screen.findByRole("button", { name: "New booking link" }));
    await userEvent.type(await screen.findByLabelText("Title"), "Deep dive");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(bookingLinksApi.create).toHaveBeenCalled());
    const call = vi.mocked(bookingLinksApi.create).mock.calls[0];
    expect(call[1]).toMatchObject({
      title: "Deep dive",
      slug: "deep-dive",
      durationMinutes: 30,
      visibility: "public",
      minimumNoticeMinutes: 240,
      bookingHorizonDays: 60,
      tasksInConflictSet: true,
      availabilityScheduleId: 1,
      bookIntoCalendarId: "cal-1",
    });
    expect(await screen.findByText("Deep dive")).toBeInTheDocument();
  });
});

describe("BookingLinksSection — copying", () => {
  it("copies the link's URL to the clipboard", async () => {
    vi.mocked(bookingLinksApi.list).mockResolvedValue([introCall]);
    render(<BookingLinksSection />);

    await userEvent.click(await screen.findByRole("button", { name: "Copy Intro call's link" }));

    await waitFor(() =>
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith(`${window.location.origin}/ada/intro-call`),
    );
  });
});

describe("BookingLinksSection — duplicating", () => {
  it("duplicates a link from its overflow menu", async () => {
    vi.mocked(bookingLinksApi.list).mockResolvedValue([introCall]);
    vi.mocked(bookingLinksApi.duplicate).mockResolvedValue({ ...introCall, id: 3, slug: "intro-call-copy", title: "Intro call (copy)" });
    render(<BookingLinksSection />);

    await userEvent.click(await screen.findByRole("button", { name: "Intro call actions" }));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Duplicate" }));

    await waitFor(() => expect(bookingLinksApi.duplicate).toHaveBeenCalledWith("token-123", 1));
    expect(await screen.findByText("Intro call (copy)")).toBeInTheDocument();
  });
});

describe("BookingLinksSection — deleting", () => {
  it("deletes a link after confirming", async () => {
    vi.mocked(bookingLinksApi.list).mockResolvedValue([introCall]);
    vi.mocked(bookingLinksApi.remove).mockResolvedValue(undefined);
    render(<BookingLinksSection />);

    await userEvent.click(await screen.findByRole("button", { name: "Intro call actions" }));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Delete" }));
    expect(await screen.findByText("Delete Intro call?")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Delete link" }));

    await waitFor(() => expect(bookingLinksApi.remove).toHaveBeenCalledWith("token-123", 1));
    await waitFor(() => expect(screen.getByText("No booking links yet.")).toBeInTheDocument());
  });
});

// #322, ADR-0084: "Changing a Slug after publishing warns that the old URL
// will stop working" — exercised through the sidebar's own Edit action
// rather than by rendering BookingLinkModal directly, so this also proves
// the overflow menu actually wires Edit to it.
describe("BookingLinksSection — editing warns before a Slug rename", () => {
  it("warns, and proceeds when confirmed", async () => {
    vi.mocked(bookingLinksApi.list).mockResolvedValue([introCall]);
    vi.spyOn(window, "confirm").mockReturnValue(true);
    vi.mocked(bookingLinksApi.update).mockResolvedValue({ ...introCall, slug: "renamed" });
    render(<BookingLinksSection />);

    await userEvent.click(await screen.findByRole("button", { name: "Intro call actions" }));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Edit" }));

    const slugInput = await screen.findByLabelText("Slug");
    await userEvent.clear(slugInput);
    await userEvent.type(slugInput, "renamed");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(window.confirm).toHaveBeenCalledWith(
      expect.stringContaining('breaks the URL published under "intro-call"'),
    );
    await waitFor(() =>
      expect(bookingLinksApi.update).toHaveBeenCalledWith("token-123", 1, expect.objectContaining({ slug: "renamed" })),
    );
  });

  it("does not submit when the rename warning is declined", async () => {
    vi.mocked(bookingLinksApi.list).mockResolvedValue([introCall]);
    vi.spyOn(window, "confirm").mockReturnValue(false);
    render(<BookingLinksSection />);

    await userEvent.click(await screen.findByRole("button", { name: "Intro call actions" }));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Edit" }));

    const slugInput = await screen.findByLabelText("Slug");
    await userEvent.clear(slugInput);
    await userEvent.type(slugInput, "renamed");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(bookingLinksApi.update).not.toHaveBeenCalled();
  });

  it("does not warn when saving without changing the Slug", async () => {
    vi.mocked(bookingLinksApi.list).mockResolvedValue([introCall]);
    const confirmSpy = vi.spyOn(window, "confirm");
    vi.mocked(bookingLinksApi.update).mockResolvedValue(introCall);
    render(<BookingLinksSection />);

    await userEvent.click(await screen.findByRole("button", { name: "Intro call actions" }));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Edit" }));
    await userEvent.click(await screen.findByRole("button", { name: "Save" }));

    expect(confirmSpy).not.toHaveBeenCalled();
    await waitFor(() => expect(bookingLinksApi.update).toHaveBeenCalled());
  });
});
