import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "./apiClient";
import { downloadBlob } from "./downloadBlob";

function okBlobResponse(): Response {
  return new Response(new Blob(["BEGIN:VCALENDAR"], { type: "text/calendar" }), {
    status: 200,
  });
}

function errorResponse(status: number): Response {
  return new Response(JSON.stringify({ error: { code: "not_found", message: "not found" } }), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("downloadBlob", () => {
  it("fetches with the bearer token and drives an anchor click through an object URL", async () => {
    const fetchMock = vi.fn().mockResolvedValue(okBlobResponse());
    vi.stubGlobal("fetch", fetchMock);

    const createObjectURL = vi.fn().mockReturnValue("blob:mock-url");
    const revokeObjectURL = vi.fn();
    vi.stubGlobal("URL", { createObjectURL, revokeObjectURL });

    const anchor = { click: vi.fn(), remove: vi.fn(), href: "", download: "" };
    const appendChild = vi.fn();
    vi.stubGlobal("document", {
      createElement: vi.fn().mockReturnValue(anchor),
      body: { appendChild },
    });

    await downloadBlob("token-123", "/api/calendars/cal-1/ics", "Personal.ics");

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/calendars/cal-1/ics",
      expect.objectContaining({
        credentials: "include",
        headers: { Authorization: "Bearer token-123" },
      }),
    );
    expect(createObjectURL).toHaveBeenCalledWith(expect.any(Blob));
    expect(anchor.href).toBe("blob:mock-url");
    expect(anchor.download).toBe("Personal.ics");
    expect(appendChild).toHaveBeenCalledWith(anchor);
    expect(anchor.click).toHaveBeenCalled();
    expect(anchor.remove).toHaveBeenCalled();
    expect(revokeObjectURL).toHaveBeenCalledWith("blob:mock-url");
  });

  it("throws an ApiError on failure without touching the DOM", async () => {
    const fetchMock = vi.fn().mockResolvedValue(errorResponse(404));
    vi.stubGlobal("fetch", fetchMock);

    const createElement = vi.fn();
    vi.stubGlobal("document", { createElement, body: { appendChild: vi.fn() } });

    await expect(
      downloadBlob("token-123", "/api/events/evt-1/ics", "Standup.ics"),
    ).rejects.toBeInstanceOf(ApiError);
    expect(createElement).not.toHaveBeenCalled();
  });
});
