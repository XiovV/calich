import { describe, expect, it } from "vitest";
import { hasSecondaryEventFields, type EventDisclosureSignals } from "./eventDisclosure";

function signals(overrides: Partial<EventDisclosureSignals> = {}): EventDisclosureSignals {
  return {
    location: "",
    description: "",
    hasRrule: false,
    reminderCount: 0,
    attachmentCount: 0,
    attendeeCount: 0,
    ...overrides,
  };
}

describe("hasSecondaryEventFields", () => {
  it("is false when every signal is empty/zero — a bare event opens collapsed", () => {
    expect(hasSecondaryEventFields(signals())).toBe(false);
  });

  it("treats a whitespace-only location/description as empty", () => {
    expect(hasSecondaryEventFields(signals({ location: "   " }))).toBe(false);
    expect(hasSecondaryEventFields(signals({ description: "\n\t" }))).toBe(false);
  });

  it("is true when location is populated", () => {
    expect(hasSecondaryEventFields(signals({ location: "Room 4" }))).toBe(true);
  });

  it("is true when description is populated", () => {
    expect(hasSecondaryEventFields(signals({ description: "Bring laptop" }))).toBe(true);
  });

  it("is true when the event recurs", () => {
    expect(hasSecondaryEventFields(signals({ hasRrule: true }))).toBe(true);
  });

  it("is true when there is at least one reminder", () => {
    expect(hasSecondaryEventFields(signals({ reminderCount: 1 }))).toBe(true);
  });

  it("is true when there is at least one attachment", () => {
    expect(hasSecondaryEventFields(signals({ attachmentCount: 1 }))).toBe(true);
  });

  it("is true when there is at least one attendee", () => {
    expect(hasSecondaryEventFields(signals({ attendeeCount: 1 }))).toBe(true);
  });
});
