import type { ImportSummary } from "../lib/importApi";
import { hasImportDetails } from "../lib/importSummary";

interface ImportSummaryDetailsProps {
  summary: ImportSummary;
  /** Name each file, for a .zip whose entries each got their own Calendar. */
  showFilenames: boolean;
}

/**
 * The line-by-line account of what an import skipped, altered, and ignored —
 * every count with the reason for it.
 *
 * Shared by the preview and the post-import result rather than living in the
 * dialog, because ADR-0030 makes the Import summary part of the feature: it
 * is a user's only view of a deliberately lossy translation, and it has to
 * read the same before the import is confirmed and after it has run (#228).
 */
export function ImportSummaryDetails({ summary, showFilenames }: ImportSummaryDetailsProps) {
  return (
    <div className="flex flex-col gap-2 text-label-sm text-ink-muted">
      {summary.files.filter(hasImportDetails).map((entrySummary) => (
        <div key={entrySummary.filename}>
          {showFilenames && <p className="font-medium text-ink">{entrySummary.filename}</p>}
          {entrySummary.skipped.map((group) => (
            <p key={`skipped-${group.reason}`}>
              Skipped {group.count} — {group.reason}
              {group.samples && group.samples.length > 0 ? ` (${group.samples.join(", ")})` : ""}
            </p>
          ))}
          {entrySummary.adjusted.map((group) => (
            <p key={`adjusted-${group.reason}`}>
              Adjusted {group.count} — {group.reason}
            </p>
          ))}
          {entrySummary.ignored.vtodo > 0 && (
            <p>
              Ignored {entrySummary.ignored.vtodo} to-do
              {entrySummary.ignored.vtodo === 1 ? "" : "s"}
            </p>
          )}
          {entrySummary.ignored.vjournal > 0 && (
            <p>
              Ignored {entrySummary.ignored.vjournal} journal entr
              {entrySummary.ignored.vjournal === 1 ? "y" : "ies"}
            </p>
          )}
          {entrySummary.ignored.vfreebusy > 0 && (
            <p>
              Ignored {entrySummary.ignored.vfreebusy} free/busy block
              {entrySummary.ignored.vfreebusy === 1 ? "" : "s"}
            </p>
          )}
          {entrySummary.attachments.imported > 0 && (
            <p>
              Imported {entrySummary.attachments.imported} attachment
              {entrySummary.attachments.imported === 1 ? "" : "s"}
            </p>
          )}
          {entrySummary.attachments.tooLarge > 0 && (
            <p>
              Skipped {entrySummary.attachments.tooLarge} attachment
              {entrySummary.attachments.tooLarge === 1 ? "" : "s"} — too large
            </p>
          )}
          {entrySummary.attachments.tooMany > 0 && (
            <p>
              Skipped {entrySummary.attachments.tooMany} attachment
              {entrySummary.attachments.tooMany === 1 ? "" : "s"} — too many on one event
            </p>
          )}
          {entrySummary.attachments.ignoredUri > 0 && (
            <p>
              Ignored {entrySummary.attachments.ignoredUri} linked attachment
              {entrySummary.attachments.ignoredUri === 1 ? "" : "s"} — not fetched
            </p>
          )}
        </div>
      ))}
    </div>
  );
}
