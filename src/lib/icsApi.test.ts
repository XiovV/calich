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

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("icsApi export pre-flight (ADR-0041)", () => {
  it("eventOversizedAttachments fetches the event's oversized-attachments endpoint", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(jsonResponse(200, { oversized: [{ filename: "huge.bin", sizeBytes: 20_000_000, eventTitle: "Standup", eventId: "evt-1" }], count: 1 }));
    vi.stubGlobal("fetch", fetchMock);

    const summary = await icsApi.eventOversizedAttachments("token-123", "evt-1");

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/events/evt-1/ics/oversized-attachments",
      expect.objectContaining({ credentials: "include" }),
    );
    expect(summary).toEqual({
      oversized: [{ filename: "huge.bin", sizeBytes: 20_000_000, eventTitle: "Standup", eventId: "evt-1" }],
      count: 1,
    });
  });

  it("calendarOversizedAttachments fetches the calendar's oversized-attachments endpoint", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, { count: 0 }));
    vi.stubGlobal("fetch", fetchMock);

    const summary = await icsApi.calendarOversizedAttachments("token-123", "cal-1");

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/calendars/cal-1/ics/oversized-attachments",
      expect.objectContaining({ credentials: "include" }),
    );
    expect(summary).toEqual({ oversized: [], count: 0 });
  });

  it("allCalendarsOversizedAttachments fetches the download-all oversized-attachments endpoint", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, { count: 0 }));
    vi.stubGlobal("fetch", fetchMock);

    await icsApi.allCalendarsOversizedAttachments("token-123");

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/calendars/ics/oversized-attachments",
      expect.objectContaining({ credentials: "include" }),
    );
  });

  it("throws an ApiError on a non-ok response", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(jsonResponse(404, { error: { code: "not_found", message: "not found" } }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(icsApi.eventOversizedAttachments("token-123", "evt-missing")).rejects.toThrow();
  });
});
