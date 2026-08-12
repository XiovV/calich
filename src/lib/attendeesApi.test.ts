import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "./apiClient";
import { attendeesApi } from "./attendeesApi";

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

describe("attendeesApi.list", () => {
  it("sends the bearer token and returns every attendee", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(
        jsonResponse(200, [{ userId: 2, name: "Jamie", response: "accepted" }]),
      );
    vi.stubGlobal("fetch", fetchMock);

    const attendees = await attendeesApi.list("token-123", "event-1");

    expect(attendees).toEqual([{ userId: 2, name: "Jamie", response: "accepted" }]);
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/events/event-1/attendees",
      expect.objectContaining({
        credentials: "include",
        headers: { Authorization: "Bearer token-123" },
      }),
    );
  });

  it("throws an ApiError on failure", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(404, { error: { code: "not_found", message: "event not found" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(attendeesApi.list("token-123", "event-1")).rejects.toMatchObject({
      code: "not_found",
      status: 404,
    });
  });
});

describe("attendeesApi.add", () => {
  it("posts userId and returns the created attendee", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(jsonResponse(201, { userId: 2, response: "needs-action" }));
    vi.stubGlobal("fetch", fetchMock);

    const attendee = await attendeesApi.add("token-123", "event-1", 2);

    expect(attendee).toEqual({ userId: 2, response: "needs-action" });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/events/event-1/attendees",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        headers: expect.objectContaining({ Authorization: "Bearer token-123" }),
        body: JSON.stringify({ userId: 2 }),
      }),
    );
  });

  it("throws an ApiError with the backend's code on a duplicate invite", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(409, {
        error: { code: "already_attendee", message: "user is already an attendee of this event" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(attendeesApi.add("token-123", "event-1", 2)).rejects.toBeInstanceOf(ApiError);
  });
});

describe("attendeesApi.addEmail", () => {
  it("posts email and returns the created attendee", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(201, { userId: null, email: "guest@example.com", response: "needs-action" }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const attendee = await attendeesApi.addEmail("token-123", "event-1", "guest@example.com");

    expect(attendee).toEqual({ userId: null, email: "guest@example.com", response: "needs-action" });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/events/event-1/attendees",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        headers: expect.objectContaining({ Authorization: "Bearer token-123" }),
        body: JSON.stringify({ email: "guest@example.com" }),
      }),
    );
  });

  it("throws an ApiError on a malformed address", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(400, { error: { code: "invalid_request", message: "email must be a valid address" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      attendeesApi.addEmail("token-123", "event-1", "not-an-email"),
    ).rejects.toBeInstanceOf(ApiError);
  });
});

describe("attendeesApi.removeEmail", () => {
  it("sends a DELETE request to the attendee's URL-encoded address", async () => {
    const fetchMock = vi.fn().mockResolvedValue(emptyResponse(204));
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      attendeesApi.removeEmail("token-123", "event-1", "guest@example.com"),
    ).resolves.toBeUndefined();
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/events/event-1/attendees/email/guest%40example.com",
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

    await expect(
      attendeesApi.removeEmail("token-123", "event-1", "guest@example.com"),
    ).rejects.toMatchObject({ code: "not_found" });
  });
});

describe("attendeesApi.addGroup", () => {
  it("posts groupId and returns every expanded attendee", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(201, [
        { userId: 2, response: "needs-action" },
        { userId: 3, response: "needs-action" },
      ]),
    );
    vi.stubGlobal("fetch", fetchMock);

    const attendees = await attendeesApi.addGroup("token-123", "event-1", 9);

    expect(attendees).toEqual([
      { userId: 2, response: "needs-action" },
      { userId: 3, response: "needs-action" },
    ]);
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/events/event-1/attendees/group",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        body: JSON.stringify({ groupId: 9 }),
      }),
    );
  });

  it("throws on failure", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(400, { error: { code: "invalid_request", message: "group not found" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(attendeesApi.addGroup("token-123", "event-1", 9)).rejects.toBeInstanceOf(
      ApiError,
    );
  });
});

describe("attendeesApi.remove", () => {
  it("sends a DELETE request to the attendee's own URL", async () => {
    const fetchMock = vi.fn().mockResolvedValue(emptyResponse(204));
    vi.stubGlobal("fetch", fetchMock);

    await expect(attendeesApi.remove("token-123", "event-1", 2)).resolves.toBeUndefined();
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/events/event-1/attendees/2",
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

    await expect(attendeesApi.remove("token-123", "event-1", 2)).rejects.toMatchObject({
      code: "not_found",
    });
  });
});

describe("attendeesApi.setResponse", () => {
  it("puts the caller's own response and returns the saved record", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(jsonResponse(200, { userId: 2, response: "accepted" }));
    vi.stubGlobal("fetch", fetchMock);

    const saved = await attendeesApi.setResponse("token-123", "event-1", "accepted");

    expect(saved).toEqual({ userId: 2, response: "accepted" });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/events/event-1/attendees/response",
      expect.objectContaining({
        method: "PUT",
        credentials: "include",
        headers: expect.objectContaining({ Authorization: "Bearer token-123" }),
        body: JSON.stringify({ response: "accepted" }),
      }),
    );
  });

  it("throws an ApiError with the backend's code on an invalid response", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(400, {
        error: {
          code: "invalid_request",
          message: 'response must be "needs-action", "accepted", "declined", or "tentative"',
        },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      attendeesApi.setResponse("token-123", "event-1", "maybe" as never),
    ).rejects.toBeInstanceOf(ApiError);
  });
});
