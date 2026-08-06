import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("./downloadBlob", () => ({ downloadBlob: vi.fn() }));

import { downloadBlob } from "./downloadBlob";
import { icsApi } from "./icsApi";

const downloadBlobMock = vi.mocked(downloadBlob);

afterEach(() => {
  downloadBlobMock.mockReset();
});

describe("icsApi.downloadEvent", () => {
  it("downloads the whole series by default", async () => {
    await icsApi.downloadEvent("token-123", "evt-1", "Standup");

    expect(downloadBlobMock).toHaveBeenCalledWith(
      "token-123",
      "/api/events/evt-1/ics",
      "Standup.ics",
    );
  });

  it("downloads a flattened occurrence when scope is occurrence", async () => {
    await icsApi.downloadEvent("token-123", "evt-1", "Standup", {
      type: "occurrence",
      occurrenceStart: new Date("2026-01-01T09:00:00.000Z"),
    });

    expect(downloadBlobMock).toHaveBeenCalledWith(
      "token-123",
      "/api/events/evt-1/ics?scope=occurrence&occurrenceStart=2026-01-01T09%3A00%3A00.000Z",
      "Standup.ics",
    );
  });

  it("falls back to a default name when the title is blank", async () => {
    await icsApi.downloadEvent("token-123", "evt-1", "   ");

    expect(downloadBlobMock).toHaveBeenCalledWith("token-123", "/api/events/evt-1/ics", "event.ics");
  });

  it("sanitizes unsafe filename characters", async () => {
    await icsApi.downloadEvent("token-123", "evt-2", "Trip/Vacation");

    expect(downloadBlobMock).toHaveBeenCalledWith(
      "token-123",
      "/api/events/evt-2/ics",
      "Trip-Vacation.ics",
    );
  });
});

describe("icsApi.downloadCalendar", () => {
  it("downloads the calendar named after it", async () => {
    await icsApi.downloadCalendar("token-123", "cal-1", "Personal");

    expect(downloadBlobMock).toHaveBeenCalledWith(
      "token-123",
      "/api/calendars/cal-1/ics",
      "Personal.ics",
    );
  });

  it("falls back to a default name when the name is blank", async () => {
    await icsApi.downloadCalendar("token-123", "cal-1", "");

    expect(downloadBlobMock).toHaveBeenCalledWith(
      "token-123",
      "/api/calendars/cal-1/ics",
      "calendar.ics",
    );
  });
});

describe("icsApi.downloadAllCalendars", () => {
  it("downloads every calendar as a zip", async () => {
    await icsApi.downloadAllCalendars("token-123");

    expect(downloadBlobMock).toHaveBeenCalledWith("token-123", "/api/calendars/ics", "calendars.zip");
  });
});
