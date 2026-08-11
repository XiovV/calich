import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "./apiClient";
import { eventsApi } from "./eventsApi";

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function emptyResponse(status: number): Response {
  return new Response(null, { status });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

const wireEvent = {
  id: "evt-1",
  calendarId: "cal-1",
  title: "Standup",
  start: "2026-01-01T09:00:00Z",
  end: "2026-01-01T10:00:00Z",
};

describe("eventsApi.list", () => {
  it("sends the bearer token and maps wire events to Event objects", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, [wireEvent]));
    vi.stubGlobal("fetch", fetchMock);

    const events = await eventsApi.list("token-123");

    expect(events).toEqual([
      {
        id: "evt-1",
        calendarId: "cal-1",
        title: "Standup",
        start: new Date("2026-01-01T09:00:00Z"),
        end: new Date("2026-01-01T10:00:00Z"),
      },
    ]);
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/events/",
      expect.objectContaining({
        credentials: "include",
        headers: { Authorization: "Bearer token-123" },
      }),
    );
  });

  it("throws an ApiError on failure", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(401, { error: { code: "unauthorized", message: "missing token" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(eventsApi.list("token-123")).rejects.toMatchObject({
      code: "unauthorized",
      status: 401,
    });
  });

  it("maps createdByName when present, and omits it when absent (#118)", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, [
        { ...wireEvent, createdByName: "alice" },
        { ...wireEvent, id: "evt-2" },
      ]),
    );
    vi.stubGlobal("fetch", fetchMock);

    const events = await eventsApi.list("token-123");

    expect(events[0].createdByName).toBe("alice");
    expect(events[1].createdByName).toBeUndefined();
  });
});

describe("eventsApi.create", () => {
  it("posts ISO timestamps and returns the mapped created event", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(201, wireEvent));
    vi.stubGlobal("fetch", fetchMock);

    const created = await eventsApi.create("token-123", {
      id: "evt-1",
      calendarId: "cal-1",
      title: "Standup",
      start: new Date("2026-01-01T09:00:00Z"),
      end: new Date("2026-01-01T10:00:00Z"),
    });

    expect(created).toEqual({
      id: "evt-1",
      calendarId: "cal-1",
      title: "Standup",
      start: new Date("2026-01-01T09:00:00Z"),
      end: new Date("2026-01-01T10:00:00Z"),
    });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/events/",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        headers: expect.objectContaining({ Authorization: "Bearer token-123" }),
        body: JSON.stringify({
          id: "evt-1",
          calendarId: "cal-1",
          title: "Standup",
          start: "2026-01-01T09:00:00.000Z",
          end: "2026-01-01T10:00:00.000Z",
          allDay: false,
          rrule: "",
        }),
      }),
    );
  });

  it("sends the rrule and maps it back on the created event", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(
        jsonResponse(201, { ...wireEvent, rrule: "FREQ=WEEKLY;BYDAY=TH" }),
      );
    vi.stubGlobal("fetch", fetchMock);

    const created = await eventsApi.create("token-123", {
      id: "evt-1",
      calendarId: "cal-1",
      title: "Standup",
      start: new Date("2026-01-01T09:00:00Z"),
      end: new Date("2026-01-01T10:00:00Z"),
      rrule: "FREQ=WEEKLY;BYDAY=TH",
    });

    expect(created.rrule).toBe("FREQ=WEEKLY;BYDAY=TH");
    const body = JSON.parse((fetchMock.mock.calls[0][1] as RequestInit).body as string);
    expect(body.rrule).toBe("FREQ=WEEKLY;BYDAY=TH");
  });

  it("throws an ApiError with the backend's code on validation failure", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(400, { error: { code: "invalid_request", message: "end must be after start" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      eventsApi.create("token-123", {
        id: "evt-1",
        calendarId: "cal-1",
        title: "Standup",
        start: new Date("2026-01-01T10:00:00Z"),
        end: new Date("2026-01-01T09:00:00Z"),
      }),
    ).rejects.toBeInstanceOf(ApiError);
  });

  // Attendees named as part of the create request itself (#187, ADR-0055),
  // rather than only reachable through attendeesApi once the Event exists.
  it("sends attendeeUserIds and attendeeGroupIds when staged", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(201, wireEvent));
    vi.stubGlobal("fetch", fetchMock);

    await eventsApi.create("token-123", {
      id: "evt-1",
      calendarId: "cal-1",
      title: "Standup",
      start: new Date("2026-01-01T09:00:00Z"),
      end: new Date("2026-01-01T10:00:00Z"),
      attendeeUserIds: [7, 8],
      attendeeGroupIds: [3],
    });

    const body = JSON.parse((fetchMock.mock.calls[0][1] as RequestInit).body as string);
    expect(body.attendeeUserIds).toEqual([7, 8]);
    expect(body.attendeeGroupIds).toEqual([3]);
  });
});

describe("eventsApi all-day serialization", () => {
  it("create sends date-only start/end for an all-day event, never toISOString", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(201, {
        id: "evt-1",
        calendarId: "cal-1",
        title: "Holiday",
        start: "2026-08-04",
        end: "2026-08-05",
        allDay: true,
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const created = await eventsApi.create("token-123", {
      id: "evt-1",
      calendarId: "cal-1",
      title: "Holiday",
      start: new Date(2026, 7, 4),
      end: new Date(2026, 7, 5),
      allDay: true,
    });

    const body = JSON.parse((fetchMock.mock.calls[0][1] as RequestInit).body as string);
    expect(body.start).toBe("2026-08-04");
    expect(body.end).toBe("2026-08-05");
    expect(body.allDay).toBe(true);

    expect(created.allDay).toBe(true);
    expect(created.start).toEqual(new Date(2026, 7, 4));
    expect(created.end).toEqual(new Date(2026, 7, 5));
  });

  it("update sends date-only start/end for an all-day event", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, {
        id: "evt-1",
        calendarId: "cal-1",
        title: "Holiday",
        start: "2026-08-04",
        end: "2026-08-05",
        allDay: true,
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await eventsApi.update("token-123", "evt-1", {
      calendarId: "cal-1",
      title: "Holiday",
      start: new Date(2026, 7, 4),
      end: new Date(2026, 7, 5),
      allDay: true,
    });

    const body = JSON.parse((fetchMock.mock.calls[0][1] as RequestInit).body as string);
    expect(body.start).toBe("2026-08-04");
    expect(body.end).toBe("2026-08-05");
    expect(body.allDay).toBe(true);
  });

  it("list round-trips an all-day event's date-only start/end without shifting the day", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, [
        {
          id: "evt-1",
          calendarId: "cal-1",
          title: "Holiday",
          start: "2026-08-04",
          end: "2026-08-05",
          allDay: true,
        },
      ]),
    );
    vi.stubGlobal("fetch", fetchMock);

    const [event] = await eventsApi.list("token-123");

    expect(event.allDay).toBe(true);
    // Local midnight on the 4th/5th, not a UTC instant that could read back
    // as a different local day in another timezone.
    expect(event.start).toEqual(new Date(2026, 7, 4));
    expect(event.end).toEqual(new Date(2026, 7, 5));
  });
});

describe("eventsApi.update", () => {
  it("patches the event and returns the mapped updated record", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, { ...wireEvent, title: "Renamed" }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const updated = await eventsApi.update("token-123", "evt-1", {
      calendarId: "cal-1",
      title: "Renamed",
      start: new Date("2026-01-01T09:00:00Z"),
      end: new Date("2026-01-01T10:00:00Z"),
    });

    expect(updated.title).toBe("Renamed");
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/events/evt-1",
      expect.objectContaining({
        method: "PATCH",
        credentials: "include",
        body: JSON.stringify({
          calendarId: "cal-1",
          title: "Renamed",
          start: "2026-01-01T09:00:00.000Z",
          end: "2026-01-01T10:00:00.000Z",
          allDay: false,
          rrule: "",
        }),
      }),
    );
  });
});

describe("eventsApi.list overrides and exceptions", () => {
  it("maps parentId/recurrenceId and exdates onto the mapped Event", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, [
        {
          ...wireEvent,
          rrule: "FREQ=DAILY",
          exdates: ["2026-01-03T09:00:00Z"],
        },
        {
          id: "evt-2",
          calendarId: "cal-1",
          title: "Standup (moved)",
          start: "2026-01-02T10:00:00Z",
          end: "2026-01-02T10:30:00Z",
          parentId: "evt-1",
          recurrenceId: "2026-01-02T09:00:00Z",
        },
      ]),
    );
    vi.stubGlobal("fetch", fetchMock);

    const events = await eventsApi.list("token-123");

    expect(events[0].exdates).toEqual([new Date("2026-01-03T09:00:00Z")]);
    expect(events[1].parentId).toBe("evt-1");
    expect(events[1].recurrenceId).toEqual(new Date("2026-01-02T09:00:00Z"));
  });
});

describe("eventsApi.create override", () => {
  it("sends parentId/recurrenceId for an override", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(201, {
        id: "evt-2",
        calendarId: "cal-1",
        title: "Standup (moved)",
        start: "2026-01-02T10:00:00Z",
        end: "2026-01-02T10:30:00Z",
        parentId: "evt-1",
        recurrenceId: "2026-01-02T09:00:00Z",
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const created = await eventsApi.create("token-123", {
      id: "evt-2",
      calendarId: "cal-1",
      title: "Standup (moved)",
      start: new Date("2026-01-02T10:00:00Z"),
      end: new Date("2026-01-02T10:30:00Z"),
      parentId: "evt-1",
      recurrenceId: new Date("2026-01-02T09:00:00Z"),
    });

    expect(created.parentId).toBe("evt-1");
    expect(created.recurrenceId).toEqual(new Date("2026-01-02T09:00:00Z"));
    const body = JSON.parse((fetchMock.mock.calls[0][1] as RequestInit).body as string);
    expect(body.parentId).toBe("evt-1");
    expect(body.recurrenceId).toBe("2026-01-02T09:00:00.000Z");
  });
});

describe("eventsApi tzid", () => {
  it("list maps a wire tzid onto the Event, and omits it when absent (Floating)", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, [
        { ...wireEvent, tzid: "Europe/Berlin" },
        wireEvent,
      ]),
    );
    vi.stubGlobal("fetch", fetchMock);

    const events = await eventsApi.list("token-123");

    expect(events[0].tzid).toBe("Europe/Berlin");
    expect(events[1].tzid).toBeUndefined();
  });

  it("create sends tzid and maps it back", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(201, { ...wireEvent, tzid: "Europe/Berlin" }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const created = await eventsApi.create("token-123", {
      id: "evt-1",
      calendarId: "cal-1",
      title: "Standup",
      start: new Date("2026-01-01T09:00:00Z"),
      end: new Date("2026-01-01T10:00:00Z"),
      tzid: "Europe/Berlin",
    });

    expect(created.tzid).toBe("Europe/Berlin");
    const body = JSON.parse((fetchMock.mock.calls[0][1] as RequestInit).body as string);
    expect(body.tzid).toBe("Europe/Berlin");
  });

  it("update sends tzid", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, { ...wireEvent, tzid: "Europe/Berlin" }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await eventsApi.update("token-123", "evt-1", {
      calendarId: "cal-1",
      title: "Standup",
      start: new Date("2026-01-01T09:00:00Z"),
      end: new Date("2026-01-01T10:00:00Z"),
      tzid: "Europe/Berlin",
    });

    const body = JSON.parse((fetchMock.mock.calls[0][1] as RequestInit).body as string);
    expect(body.tzid).toBe("Europe/Berlin");
  });
});

describe("eventsApi reminders", () => {
  it("list maps wire reminders onto the Event, and omits it when absent", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, [
        { ...wireEvent, reminders: [{ offsetMinutes: 10, channel: "notification" }] },
        wireEvent,
      ]),
    );
    vi.stubGlobal("fetch", fetchMock);

    const events = await eventsApi.list("token-123");

    expect(events[0].reminders).toEqual([{ offsetMinutes: 10, channel: "notification" }]);
    expect(events[1].reminders).toBeUndefined();
  });

  it("create sends reminders and maps them back", async () => {
    const reminders = [
      { offsetMinutes: 10, channel: "notification" as const },
      { offsetMinutes: 1440, channel: "email" as const },
    ];
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(201, { ...wireEvent, reminders }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const created = await eventsApi.create("token-123", {
      id: "evt-1",
      calendarId: "cal-1",
      title: "Standup",
      start: new Date("2026-01-01T09:00:00Z"),
      end: new Date("2026-01-01T10:00:00Z"),
      reminders,
    });

    expect(created.reminders).toEqual(reminders);
    const body = JSON.parse((fetchMock.mock.calls[0][1] as RequestInit).body as string);
    expect(body.reminders).toEqual(reminders);
  });

  it("update sends reminders", async () => {
    const reminders = [{ offsetMinutes: 30, channel: "email" as const }];
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, { ...wireEvent, reminders }));
    vi.stubGlobal("fetch", fetchMock);

    await eventsApi.update("token-123", "evt-1", {
      calendarId: "cal-1",
      title: "Standup",
      start: new Date("2026-01-01T09:00:00Z"),
      end: new Date("2026-01-01T10:00:00Z"),
      reminders,
    });

    const body = JSON.parse((fetchMock.mock.calls[0][1] as RequestInit).body as string);
    expect(body.reminders).toEqual(reminders);
  });
});

describe("eventsApi.addException", () => {
  it("posts the occurrence start", async () => {
    const fetchMock = vi.fn().mockResolvedValue(emptyResponse(204));
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      eventsApi.addException(
        "token-123",
        "evt-1",
        new Date("2026-01-02T09:00:00Z"),
      ),
    ).resolves.toBeUndefined();

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/events/evt-1/exceptions",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        body: JSON.stringify({ occurrenceStart: "2026-01-02T09:00:00.000Z" }),
      }),
    );
  });

  it("throws on failure", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(404, { error: { code: "not_found", message: "event not found" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      eventsApi.addException("token-123", "evt-1", new Date()),
    ).rejects.toMatchObject({ code: "not_found" });
  });
});

describe("eventsApi.reparentSeries", () => {
  it("posts the new parent and split boundary", async () => {
    const fetchMock = vi.fn().mockResolvedValue(emptyResponse(204));
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      eventsApi.reparentSeries(
        "token-123",
        "old-master",
        "new-master",
        new Date("2026-01-05T00:00:00Z"),
      ),
    ).resolves.toBeUndefined();

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/events/old-master/reparent",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        body: JSON.stringify({
          newParentId: "new-master",
          fromStart: "2026-01-05T00:00:00.000Z",
        }),
      }),
    );
  });
});

describe("eventsApi.remove", () => {
  it("sends a DELETE request", async () => {
    const fetchMock = vi.fn().mockResolvedValue(emptyResponse(204));
    vi.stubGlobal("fetch", fetchMock);

    await expect(eventsApi.remove("token-123", "evt-1")).resolves.toBeUndefined();
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/events/evt-1",
      expect.objectContaining({
        method: "DELETE",
        credentials: "include",
        headers: { Authorization: "Bearer token-123" },
      }),
    );
  });

  it("throws on failure", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(404, { error: { code: "not_found", message: "event not found" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(eventsApi.remove("token-123", "evt-1")).rejects.toMatchObject({
      code: "not_found",
    });
  });
});
