import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import type { User } from "../lib/authApi";
import { useAuthStore } from "../lib/authStore";
import { defaultCustomRecurrence } from "../lib/customRecurrence";
import { CustomRecurrenceDialog } from "./CustomRecurrenceDialog";

// 2026-08-18 is a Tuesday.
const START = new Date(2026, 7, 18, 9, 0);

const baseUser: User = {
  id: 1,
  name: "Ada",
  mustChangePassword: false,
  email: "ada@example.com",
  emailReminderChannelAvailable: false,
  invitationRepliesConfigured: false,
  syncedDeviceRemindersEnabled: false,
  weekStart: 1,
  defaultView: "week",
  timeFormat: "24h",
  workingHoursStart: null,
  workingHoursEnd: null,
};

function seedWeekStart(weekStart: number) {
  useAuthStore.setState({
    status: "authenticated",
    user: { ...baseUser, weekStart },
    accessToken: "token-123",
  });
}

function renderDialog() {
  render(
    <CustomRecurrenceDialog
      initial={defaultCustomRecurrence(START)}
      start={START}
      onConfirm={vi.fn()}
      onClose={vi.fn()}
    />,
  );
}

function chipOrder() {
  return screen
    .getAllByRole("button")
    .filter((button) => button.hasAttribute("aria-pressed"))
    .map((button) => button.getAttribute("aria-label"));
}

describe("CustomRecurrenceDialog weekday chips", () => {
  it("reads Monday-first when Week start is Monday", () => {
    seedWeekStart(1);
    renderDialog();

    expect(chipOrder()).toEqual([
      "Monday",
      "Tuesday",
      "Wednesday",
      "Thursday",
      "Friday",
      "Saturday",
      "Sunday",
    ]);
  });

  it("reads Sunday-first when Week start is Sunday", () => {
    seedWeekStart(0);
    renderDialog();

    expect(chipOrder()).toEqual([
      "Sunday",
      "Monday",
      "Tuesday",
      "Wednesday",
      "Thursday",
      "Friday",
      "Saturday",
    ]);
  });

  it("keeps each chip's full weekday name as its accessible label", () => {
    seedWeekStart(1);
    renderDialog();

    expect(screen.getByRole("button", { name: "Wednesday" })).toBeInTheDocument();
  });
});
