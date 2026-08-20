import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
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
  const onConfirm = vi.fn();
  render(
    <CustomRecurrenceDialog
      initial={defaultCustomRecurrence(START)}
      start={START}
      onConfirm={onConfirm}
      onClose={vi.fn()}
    />,
  );
  return { onConfirm };
}

function repeatEveryInput() {
  return screen.getByLabelText("Repeat every");
}

function doneButton() {
  return screen.getByRole("button", { name: "Done" });
}

async function setRepeatEvery(value: string) {
  const input = repeatEveryInput();
  await userEvent.clear(input);
  if (value !== "") await userEvent.type(input, value);
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

describe("CustomRecurrenceDialog interval validation", () => {
  it("accepts a valid interval and includes it in the built rule", async () => {
    seedWeekStart(1);
    const { onConfirm } = renderDialog();

    await setRepeatEvery("2");
    expect(doneButton()).toBeEnabled();

    await userEvent.click(doneButton());
    expect(onConfirm).toHaveBeenCalledWith(expect.stringContaining("INTERVAL=2"));
  });

  it("omits INTERVAL for an interval of 1", async () => {
    seedWeekStart(1);
    const { onConfirm } = renderDialog();

    await setRepeatEvery("1");
    await userEvent.click(doneButton());

    expect(onConfirm).toHaveBeenCalledWith(expect.not.stringContaining("INTERVAL"));
  });

  it.each([
    ["an interval of 0", "0"],
    ["a negative interval", "-3"],
    ["an empty field", ""],
  ])("refuses %s with a visible, disabling message", async (_label, value) => {
    seedWeekStart(1);
    const { onConfirm } = renderDialog();

    await setRepeatEvery(value);

    const input = repeatEveryInput();
    expect(input).toHaveAttribute("aria-invalid", "true");
    expect(doneButton()).toBeDisabled();

    const message = screen.getByRole("alert");
    expect(message).toHaveTextContent("Enter a whole number of 1 or more.");

    await userEvent.click(doneButton());
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it("re-enables Done once a valid interval replaces an invalid one", async () => {
    seedWeekStart(1);
    renderDialog();

    await setRepeatEvery("0");
    expect(doneButton()).toBeDisabled();

    await setRepeatEvery("3");
    expect(doneButton()).toBeEnabled();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});
