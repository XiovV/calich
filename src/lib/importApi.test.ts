import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "./apiClient";
import { importApi } from "./importApi";
import { useWorkspacesStore } from "./workspacesStore";

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

const wireSummary = {
  files: [
    {
      filename: "invite.ics",
      calendarName: "Imported Calendar",
      calendarId: "cal-1",
      eventCount: 3,
      rangeStart: "2026-01-01T09:00:00Z",
      rangeEnd: "2026-03-01T09:00:00Z",
      skipped: [{ reason: "unparseable series", count: 1, samples: ["Broken event"] }],
      adjusted: [{ reason: "unresolvable timezone downgraded to a Floating Event", count: 2 }],
      ignored: { vtodo: 1, vjournal: 0, vfreebusy: 0 },
      reminders: { notification: 4, email: 1 },
      attachments: { imported: 2, tooLarge: 0, tooMany: 0, ignoredUri: 0 },
    },
  ],
};

beforeEach(() => {
  useWorkspacesStore.setState({ activeWorkspaceId: 7 });
});

afterEach(() => {
  vi.unstubAllGlobals();
  useWorkspacesStore.setState({ activeWorkspaceId: null });
});

describe("importApi.preview", () => {
  it("POSTs a multipart upload with dryRun=1 and maps the response", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, wireSummary));
    vi.stubGlobal("fetch", fetchMock);

    const file = new File(["BEGIN:VCALENDAR"], "invite.ics", { type: "text/calendar" });
    const summary = await importApi.preview("token-123", file, [
      { filename: "invite.ics", action: "new" },
    ]);

    expect(summary.files).toHaveLength(1);
    expect(summary.files[0]).toMatchObject({
      filename: "invite.ics",
      calendarName: "Imported Calendar",
      calendarId: "cal-1",
      eventCount: 3,
      skipped: [{ reason: "unparseable series", count: 1, samples: ["Broken event"] }],
      adjusted: [{ reason: "unresolvable timezone downgraded to a Floating Event", count: 2 }],
      ignored: { vtodo: 1, vjournal: 0, vfreebusy: 0 },
      reminders: { notification: 4, email: 1 },
      attachments: { imported: 2, tooLarge: 0, tooMany: 0, ignoredUri: 0 },
    });
    expect(summary.files[0].rangeStart).toEqual(new Date("2026-01-01T09:00:00Z"));
    expect(summary.files[0].rangeEnd).toEqual(new Date("2026-03-01T09:00:00Z"));

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe("/api/calendars/import?dryRun=1");
    expect(init.method).toBe("POST");
    expect(init.credentials).toBe("include");
    expect(init.headers).toEqual({ Authorization: "Bearer token-123", "X-Workspace-Id": "7" });

    const body = init.body as FormData;
    expect(body.get("file")).toBeInstanceOf(File);
    expect((body.get("file") as File).name).toBe("invite.ics");
    expect(body.get("targets")).toBe(JSON.stringify({ entries: [{ filename: "invite.ics", action: "new" }] }));
  });

  it("leaves rangeStart/rangeEnd undefined and defaults skipped/adjusted to empty arrays when absent", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, {
        files: [
          {
            filename: "invite.ics",
            calendarName: "Imported Calendar",
            eventCount: 0,
            ignored: { vtodo: 0, vjournal: 0, vfreebusy: 0 },
            reminders: { notification: 0, email: 0 },
            attachments: { imported: 0, tooLarge: 0, tooMany: 0, ignoredUri: 0 },
          },
        ],
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const file = new File(["BEGIN:VCALENDAR"], "invite.ics");
    const summary = await importApi.preview("token-123", file, [
      { filename: "invite.ics", action: "new" },
    ]);

    expect(summary.files[0].rangeStart).toBeUndefined();
    expect(summary.files[0].rangeEnd).toBeUndefined();
    expect(summary.files[0].calendarId).toBeUndefined();
    expect(summary.files[0].skipped).toEqual([]);
    expect(summary.files[0].adjusted).toEqual([]);
  });

  it("throws an ApiError on failure", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(400, { error: { code: "invalid_request", message: "targets must be valid JSON" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const file = new File(["BEGIN:VCALENDAR"], "invite.ics");
    await expect(importApi.preview("token-123", file, [])).rejects.toBeInstanceOf(ApiError);
  });
});

describe("importApi.commit", () => {
  it("POSTs a multipart upload with dryRun=0", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, wireSummary));
    vi.stubGlobal("fetch", fetchMock);

    const file = new File(["BEGIN:VCALENDAR"], "invite.ics");
    await importApi.commit("token-123", file, [{ filename: "invite.ics", action: "new", name: "Trip" }]);

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe("/api/calendars/import?dryRun=0");
    const body = init.body as FormData;
    expect(body.get("targets")).toBe(
      JSON.stringify({ entries: [{ filename: "invite.ics", action: "new", name: "Trip" }] }),
    );
  });
});
