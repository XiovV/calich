import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "./apiClient";
import { calendarsApi } from "./calendarsApi";

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

describe("calendarsApi.list", () => {
  it("sends the bearer token and returns the calendars", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, [{ id: "cal-1", name: "Personal", color: "peacock" }]),
    );
    vi.stubGlobal("fetch", fetchMock);

    const calendars = await calendarsApi.list("token-123");

    expect(calendars).toEqual([{ id: "cal-1", name: "Personal", color: "peacock" }]);
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/calendars/",
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

    await expect(calendarsApi.list("token-123")).rejects.toMatchObject({
      code: "unauthorized",
      status: 401,
    });
  });
});

describe("calendarsApi.create", () => {
  it("posts the calendar and returns the created record", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(201, { id: "cal-1", name: "Personal", color: "peacock" }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const created = await calendarsApi.create("token-123", {
      id: "cal-1",
      name: "Personal",
      color: "peacock",
    });

    expect(created).toEqual({ id: "cal-1", name: "Personal", color: "peacock" });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/calendars/",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        headers: expect.objectContaining({ Authorization: "Bearer token-123" }),
        body: JSON.stringify({ id: "cal-1", name: "Personal", color: "peacock" }),
      }),
    );
  });

  it("throws an ApiError with the backend's code on validation failure", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(400, { error: { code: "invalid_request", message: "color is not a recognized calendar color" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      calendarsApi.create("token-123", { id: "cal-1", name: "Personal", color: "peacock" }),
    ).rejects.toBeInstanceOf(ApiError);
  });
});

describe("calendarsApi.update", () => {
  it("patches the calendar and returns the updated record", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, { id: "cal-1", name: "Renamed", color: "tomato" }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const updated = await calendarsApi.update("token-123", "cal-1", {
      name: "Renamed",
      color: "tomato",
    });

    expect(updated).toEqual({ id: "cal-1", name: "Renamed", color: "tomato" });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/calendars/cal-1",
      expect.objectContaining({
        method: "PATCH",
        credentials: "include",
        body: JSON.stringify({ name: "Renamed", color: "tomato" }),
      }),
    );
  });
});

describe("calendarsApi.remove", () => {
  it("sends a DELETE request", async () => {
    const fetchMock = vi.fn().mockResolvedValue(emptyResponse(204));
    vi.stubGlobal("fetch", fetchMock);

    await expect(calendarsApi.remove("token-123", "cal-1")).resolves.toBeUndefined();
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/calendars/cal-1",
      expect.objectContaining({
        method: "DELETE",
        credentials: "include",
        headers: { Authorization: "Bearer token-123" },
      }),
    );
  });

  it("throws on failure", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(404, { error: { code: "not_found", message: "calendar not found" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(calendarsApi.remove("token-123", "cal-1")).rejects.toMatchObject({
      code: "not_found",
    });
  });
});
