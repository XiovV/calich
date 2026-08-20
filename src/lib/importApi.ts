import { authedFetch, errorFromResponse } from "./apiClient";
import { workspaceHeaders } from "./workspaceHeaders";

export interface ImportTarget {
  filename: string;
  action: "new" | "existing" | "skip";
  name?: string;
  calendarId?: string;
}

export interface SkippedGroup {
  reason: string;
  count: number;
  samples?: string[];
}

export interface AdjustedGroup {
  reason: string;
  count: number;
}

export interface IgnoredCounts {
  vtodo: number;
  vjournal: number;
  vfreebusy: number;
}

export interface ReminderCounts {
  notification: number;
  email: number;
}

/** The four outcomes an ATTACH property can have on import (#135, ADR-0040):
 * imported, too large for MAX_ATTACHMENT_SIZE, past MAX_ATTACHMENTS_PER_EVENT,
 * or a URI ATTACH ignored outright (never fetched). */
export interface AttachmentCounts {
  imported: number;
  tooLarge: number;
  tooMany: number;
  ignoredUri: number;
}

export interface ImportFileSummary {
  filename: string;
  calendarName: string;
  calendarId?: string;
  eventCount: number;
  rangeStart?: Date;
  rangeEnd?: Date;
  skipped: SkippedGroup[];
  adjusted: AdjustedGroup[];
  ignored: IgnoredCounts;
  reminders: ReminderCounts;
  attachments: AttachmentCounts;
}

export interface ImportSummary {
  files: ImportFileSummary[];
}

interface ImportFileSummaryWire {
  filename: string;
  calendarName: string;
  calendarId?: string;
  eventCount: number;
  rangeStart?: string;
  rangeEnd?: string;
  skipped?: SkippedGroup[];
  adjusted?: AdjustedGroup[];
  ignored: IgnoredCounts;
  reminders: ReminderCounts;
  attachments: AttachmentCounts;
}

interface ImportSummaryWire {
  files: ImportFileSummaryWire[];
}

function fromWire(wire: ImportSummaryWire): ImportSummary {
  return {
    files: wire.files.map((file) => ({
      filename: file.filename,
      calendarName: file.calendarName,
      calendarId: file.calendarId || undefined,
      eventCount: file.eventCount,
      rangeStart: file.rangeStart ? new Date(file.rangeStart) : undefined,
      rangeEnd: file.rangeEnd ? new Date(file.rangeEnd) : undefined,
      skipped: file.skipped ?? [],
      adjusted: file.adjusted ?? [],
      ignored: file.ignored,
      reminders: file.reminders,
      attachments: file.attachments,
    })),
  };
}

async function upload(
  accessToken: string,
  file: File,
  targets: ImportTarget[],
  dryRun: boolean,
): Promise<ImportSummary> {
  const formData = new FormData();
  formData.append("file", file, file.name);
  formData.append("targets", JSON.stringify({ entries: targets }));

  // No Content-Type header here — the browser sets multipart/form-data plus
  // its boundary for a FormData body; setting it manually would drop the
  // boundary and break parsing on the Go side.
  const response = await authedFetch(accessToken, `/api/calendars/import?dryRun=${dryRun ? "1" : "0"}`, {
    method: "POST",
    credentials: "include",
    headers: workspaceHeaders(),
    body: formData,
  });
  if (!response.ok) throw await errorFromResponse(response);

  return fromWire((await response.json()) as ImportSummaryWire);
}

export const importApi = {
  /** dryRun=1: parses and validates without writing anything (#77, ADR-0030). */
  preview(accessToken: string, file: File, targets: ImportTarget[]): Promise<ImportSummary> {
    return upload(accessToken, file, targets, true);
  },

  /** dryRun=0: writes. Re-sends the same File the preview parsed — the
   * browser still holds it, so there's no server-side staging to resume. */
  commit(accessToken: string, file: File, targets: ImportTarget[]): Promise<ImportSummary> {
    return upload(accessToken, file, targets, false);
  },
};
