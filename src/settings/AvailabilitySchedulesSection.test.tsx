import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

// Same convention as ConnectionsSection.test.tsx: the *Api module is mocked,
// the store is real — this covers the Settings section's wiring (#320,
// ADR-0085), not the request/response shape itself.
vi.mock("../lib/availabilitySchedulesApi", async () => {
  const actual = await vi.importActual<typeof import("../lib/availabilitySchedulesApi")>(
    "../lib/availabilitySchedulesApi",
  );
  return {
    ...actual,
    availabilitySchedulesApi: {
      list: vi.fn(),
      create: vi.fn(),
      update: vi.fn(),
      remove: vi.fn(),
    },
  };
});

const { availabilitySchedulesApi } = await import("../lib/availabilitySchedulesApi");
const { useAuthStore } = await import("../lib/authStore");
const { useAvailabilitySchedulesStore } = await import("../lib/availabilitySchedulesStore");
const { AvailabilitySchedulesSection } = await import("./AvailabilitySchedulesSection");

const defaultSchedule = {
  id: 1,
  name: "Default",
  tzid: "Etc/UTC",
  ranges: [
    { weekday: 1, startMinute: 9 * 60, endMinute: 17 * 60 },
    { weekday: 2, startMinute: 9 * 60, endMinute: 17 * 60 },
  ],
};

beforeEach(() => {
  vi.clearAllMocks();
  useAuthStore.setState({ status: "authenticated", accessToken: "token-123" });
  useAvailabilitySchedulesStore.setState({ schedules: [] });
  vi.mocked(availabilitySchedulesApi.list).mockResolvedValue([]);
});

describe("AvailabilitySchedulesSection — listing", () => {
  it("shows a placeholder when there are no schedules", async () => {
    render(<AvailabilitySchedulesSection />);

    expect(await screen.findByText("No availability schedules yet.")).toBeInTheDocument();
  });

  it("lists a schedule with its timezone and per-weekday ranges", async () => {
    vi.mocked(availabilitySchedulesApi.list).mockResolvedValue([defaultSchedule]);
    render(<AvailabilitySchedulesSection />);

    expect(await screen.findByText("Default")).toBeInTheDocument();
    expect(screen.getByText("Etc/UTC")).toBeInTheDocument();
    expect(screen.getByText("Mon")).toBeInTheDocument();
    expect(screen.getByText("Tue")).toBeInTheDocument();
  });

  it("shows an empty-schedule notice for one with no ranges", async () => {
    vi.mocked(availabilitySchedulesApi.list).mockResolvedValue([{ ...defaultSchedule, ranges: [] }]);
    render(<AvailabilitySchedulesSection />);

    expect(
      await screen.findByText("No slots yet — this schedule offers nothing to book."),
    ).toBeInTheDocument();
  });
});

describe("AvailabilitySchedulesSection — creating", () => {
  it("creates a new schedule from the dialog", async () => {
    render(<AvailabilitySchedulesSection />);
    await waitFor(() => expect(availabilitySchedulesApi.list).toHaveBeenCalled());

    vi.mocked(availabilitySchedulesApi.create).mockResolvedValue({
      id: 2,
      name: "Evenings",
      tzid: "Etc/UTC",
      ranges: [],
    });

    await userEvent.click(await screen.findByRole("button", { name: "New schedule" }));
    await userEvent.type(await screen.findByLabelText("Name"), "Evenings");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(availabilitySchedulesApi.create).toHaveBeenCalledWith("token-123", "Evenings", expect.any(String), []),
    );
    expect(await screen.findByText("Evenings")).toBeInTheDocument();
  });

  it("disables Save while any range has an end time at or before its start time", async () => {
    render(<AvailabilitySchedulesSection />);
    await userEvent.click(await screen.findByRole("button", { name: "New schedule" }));
    await userEvent.type(await screen.findByLabelText("Name"), "Bad range");

    await userEvent.click(screen.getByRole("button", { name: "Add a range on Monday" }));
    fireEvent.change(screen.getByLabelText("Monday range start"), { target: { value: "17:00" } });
    fireEvent.change(screen.getByLabelText("Monday range end"), { target: { value: "09:00" } });

    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
  });
});

describe("AvailabilitySchedulesSection — deleting", () => {
  it("deletes a schedule after confirming", async () => {
    vi.mocked(availabilitySchedulesApi.list).mockResolvedValue([defaultSchedule]);
    vi.mocked(availabilitySchedulesApi.remove).mockResolvedValue(undefined);
    render(<AvailabilitySchedulesSection />);

    await userEvent.click(await screen.findByRole("button", { name: "Delete Default" }));
    expect(await screen.findByText("Delete Default?")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Delete schedule" }));

    await waitFor(() => expect(availabilitySchedulesApi.remove).toHaveBeenCalledWith("token-123", 1));
    await waitFor(() => expect(screen.getByText("No availability schedules yet.")).toBeInTheDocument());
  });

  it("does nothing when the delete dialog is cancelled", async () => {
    vi.mocked(availabilitySchedulesApi.list).mockResolvedValue([defaultSchedule]);
    render(<AvailabilitySchedulesSection />);

    await userEvent.click(await screen.findByRole("button", { name: "Delete Default" }));
    await userEvent.click(await screen.findByRole("button", { name: "Cancel" }));

    expect(availabilitySchedulesApi.remove).not.toHaveBeenCalled();
    expect(screen.getByText("Default")).toBeInTheDocument();
  });
});
