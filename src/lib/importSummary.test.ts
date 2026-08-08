import { describe, expect, it } from "vitest";
import type { ImportSummary } from "./importApi";
import { formatImportSummaryLine, formatReminderLine, summarizeImport } from "./importSummary";

function fileSummary(overrides: Partial<ImportSummary["files"][number]> = {}) {
  return {
    filename: "invite.ics",
    calendarName: "Imported Calendar",
    calendarId: undefined,
    eventCount: 0,
    rangeStart: undefined,
    rangeEnd: undefined,
    skipped: [],
    adjusted: [],
    ignored: { vtodo: 0, vjournal: 0, vfreebusy: 0 },
    reminders: { notification: 0, email: 0 },
    attachments: { imported: 0, tooLarge: 0, tooMany: 0, ignoredUri: 0 },
    ...overrides,
  };
}

describe("summarizeImport", () => {
  it("sums counts across every file", () => {
    const summary: ImportSummary = {
      files: [
        fileSummary({
          eventCount: 1200,
          skipped: [{ reason: "unparseable series", count: 5 }],
          adjusted: [{ reason: "unresolvable timezone downgraded to a Floating Event", count: 40 }],
          ignored: { vtodo: 2, vjournal: 0, vfreebusy: 1 },
          reminders: { notification: 10, email: 3 },
          attachments: { imported: 2, tooLarge: 1, tooMany: 0, ignoredUri: 1 },
        }),
        fileSummary({
          filename: "second.ics",
          eventCount: 647,
          skipped: [{ reason: "missing DTSTART", count: 7 }],
          ignored: { vtodo: 1, vjournal: 0, vfreebusy: 0 },
          reminders: { notification: 5, email: 0 },
          attachments: { imported: 1, tooLarge: 0, tooMany: 0, ignoredUri: 0 },
        }),
      ],
    };

    expect(summarizeImport(summary)).toEqual({
      eventCount: 1847,
      skippedCount: 12,
      adjustedCount: 40,
      ignoredCount: 4,
      reminders: { notification: 15, email: 3 },
      attachmentsImported: 3,
    });
  });

  it("returns all zeros for an empty file list", () => {
    expect(summarizeImport({ files: [] })).toEqual({
      eventCount: 0,
      skippedCount: 0,
      adjustedCount: 0,
      ignoredCount: 0,
      reminders: { notification: 0, email: 0 },
      attachmentsImported: 0,
    });
  });
});

describe("formatImportSummaryLine", () => {
  it("formats every non-zero category with thousands separators", () => {
    const line = formatImportSummaryLine({
      eventCount: 1847,
      skippedCount: 12,
      adjustedCount: 40,
      ignoredCount: 3,
      reminders: { notification: 0, email: 0 },
      attachmentsImported: 2,
    });

    expect(line).toBe("1,847 events · 12 skipped · 40 adjusted · 3 ignored · 2 attachments");
  });

  it("omits zero categories and singularizes a single event or attachment", () => {
    const line = formatImportSummaryLine({
      eventCount: 1,
      skippedCount: 0,
      adjustedCount: 0,
      ignoredCount: 0,
      reminders: { notification: 0, email: 0 },
      attachmentsImported: 1,
    });

    expect(line).toBe("1 event · 1 attachment");
  });
});

describe("formatReminderLine", () => {
  it("pluralizes each channel independently", () => {
    expect(formatReminderLine({ notification: 4, email: 1 })).toBe("4 notifications · 1 email");
  });

  it("handles zero counts for both channels", () => {
    expect(formatReminderLine({ notification: 0, email: 0 })).toBe("0 notifications · 0 emails");
  });
});
