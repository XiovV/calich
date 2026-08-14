import { authedFetch, errorFromResponse } from "./apiClient";
import { downloadBlob } from "./downloadBlob";

/** One Attachment an export's Export summary pre-flight found too large to
 * inline (ADR-0041) — named by filename, size, and the Event it hangs off. */
export interface OversizedAttachment {
  filename: string;
  sizeBytes: number;
  eventTitle: string;
  eventId: string;
}

/** The Export summary pre-flight's result (ADR-0041): oversized carries up
 * to a handful of samples (capped server-side), count is the true total —
 * a caller with more oversized Attachments than fit in oversized still
 * knows how many there are. */
export interface ExportSummary {
  oversized: OversizedAttachment[];
  count: number;
}

interface ExportSummaryWire {
  oversized?: OversizedAttachment[];
  count: number;
}

function fromExportSummaryWire(wire: ExportSummaryWire): ExportSummary {
  return { oversized: wire.oversized ?? [], count: wire.count };
}

async function fetchExportSummary(accessToken: string, url: string): Promise<ExportSummary> {
  const response = await authedFetch(accessToken, url, { credentials: "include" });
  if (!response.ok) throw await errorFromResponse(response);
  return fromExportSummaryWire((await response.json()) as ExportSummaryWire);
}

/**
 * A filename built client-side from a Calendar/Event name — sidesteps RFC
 * 5987 escaping on the Go side entirely (issue #78). Mirrors
 * sanitizeICSFilename in server/internal/handlers/ics.go: replaces "/",
 * "\", and control characters with "-", falling back to `fallback` when
 * name is blank (after trimming). Keep the two in sync if either changes.
 */
function sanitizeFilename(name: string, fallback: string): string {
  const trimmed = name.trim();
  const base = trimmed === "" ? fallback : trimmed;
  return Array.from(base)
    .map((char) => {
      const code = char.codePointAt(0) ?? 0;
      return char === "/" || char === "\\" || code < 0x20 || code === 0x7f ? "-" : char;
    })
    .join("");
}

/** Which of an Event's Occurrences an export covers: the whole series, or
 * one flattened Occurrence named by its start. The download and its Export
 * summary pre-flight take the same value, so the two can never describe
 * different files (#217). */
export type EventExportScope = { type: "all" } | { type: "occurrence"; occurrenceStart: Date };

/** The scope/occurrenceStart query string GET /api/events/{id}/ics and its
 * oversized-attachments pre-flight both read. */
function eventScopeQuery(scope: EventExportScope): string {
  if (scope.type === "all") return "";
  return `?${new URLSearchParams({
    scope: "occurrence",
    occurrenceStart: scope.occurrenceStart.toISOString(),
  }).toString()}`;
}

export const icsApi = {
  /**
   * Downloads a single Event as .ics — the whole series (scope "all", the
   * default) or one flattened Occurrence (scope "occurrence", requiring the
   * Occurrence's start as its RECURRENCE-ID) — mirroring the two GET
   * /api/events/{id}/ics scopes (#76).
   */
  async downloadEvent(
    accessToken: string,
    eventId: string,
    title: string,
    scope: EventExportScope = { type: "all" },
  ): Promise<void> {
    await downloadBlob(
      accessToken,
      `/api/events/${eventId}/ics${eventScopeQuery(scope)}`,
      `${sanitizeFilename(title, "event")}.ics`,
    );
  },

  /** Downloads one Calendar (every Event in it) as .ics. */
  async downloadCalendar(accessToken: string, calendarId: string, name: string): Promise<void> {
    await downloadBlob(
      accessToken,
      `/api/calendars/${calendarId}/ics`,
      `${sanitizeFilename(name, "calendar")}.ics`,
    );
  },

  /** Downloads every Calendar as a .zip of .ics files. */
  async downloadAllCalendars(accessToken: string): Promise<void> {
    await downloadBlob(accessToken, "/api/calendars/ics", "calendars.zip");
  },

  /**
   * The Export summary pre-flight (ADR-0041) for a single Event's download:
   * which Attachments (if any) would be omitted for being too large to
   * inline. Metadata-only — no file is generated. Takes the same scope the
   * download does, because both scopes inline: a Calendar file describing one
   * Occurrence carries its series' Attachments too (#217).
   */
  eventOversizedAttachments(
    accessToken: string,
    eventId: string,
    scope: EventExportScope = { type: "all" },
  ): Promise<ExportSummary> {
    return fetchExportSummary(
      accessToken,
      `/api/events/${eventId}/ics/oversized-attachments${eventScopeQuery(scope)}`,
    );
  },

  /** The Export summary pre-flight for one Calendar's download. */
  calendarOversizedAttachments(accessToken: string, calendarId: string): Promise<ExportSummary> {
    return fetchExportSummary(accessToken, `/api/calendars/${calendarId}/ics/oversized-attachments`);
  },

  /** The Export summary pre-flight for the download-all zip. */
  allCalendarsOversizedAttachments(accessToken: string): Promise<ExportSummary> {
    return fetchExportSummary(accessToken, "/api/calendars/ics/oversized-attachments");
  },
};
