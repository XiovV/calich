import { useState } from "react";
import { format } from "date-fns";
import { Dialog } from "@base-ui/react/dialog";
import { calendarPickerLabel, canWriteCalendarEvents, type Calendar } from "../lib/calendar";
import type { ImportFileSummary, ImportSummary, ImportTarget } from "../lib/importApi";
import {
  formatImportSummaryLine,
  formatReminderLine,
  hasAnyImportDetails,
  summarizeImport,
} from "../lib/importSummary";
import { ImportSummaryDetails } from "./ImportSummaryDetails";
import { Button } from "../components/ui/Button";
import { buttonClasses } from "../components/ui/buttonClasses";
import { Checkbox } from "../components/ui/Checkbox";
import { Input } from "../components/ui/Input";
import { Select } from "../components/ui/Select";

type EntryAction = "new" | "existing" | "skip";

interface EntryDraft {
  filename: string;
  action: EntryAction;
  /** The proposed Calendar name — editable while action is "new"; ignored
   * (the existing Calendar keeps its own name) once action is "existing". */
  name: string;
  calendarId: string;
}

function initialDrafts(files: ImportFileSummary[]): EntryDraft[] {
  return files.map((file) => ({
    filename: file.filename,
    action: "new",
    name: file.calendarName,
    calendarId: "",
  }));
}

function formatRange(file: ImportFileSummary): string {
  if (!file.rangeStart || !file.rangeEnd) return "No events";
  return `${format(file.rangeStart, "MMM d, yyyy")} – ${format(file.rangeEnd, "MMM d, yyyy")}`;
}

const NEW_CALENDAR_VALUE = "__new__";

interface ImportPreviewDialogProps {
  file: File;
  isZip: boolean;
  summary: ImportSummary;
  calendars: Calendar[];
  isSubmitting: boolean;
  onConfirm: (targets: ImportTarget[]) => void;
  onClose: () => void;
}

/**
 * The preview step of the import flow (issue #79): shows what a dryRun
 * request found — per-file Calendar name/event count/date range, the
 * aggregate Import summary, and reminder counts by Channel — then lets the
 * user pick each entry's destination before confirming the real write.
 */
export function ImportPreviewDialog({
  file,
  isZip,
  summary,
  calendars,
  isSubmitting,
  onConfirm,
  onClose,
}: ImportPreviewDialogProps) {
  const [drafts, setDrafts] = useState<EntryDraft[]>(() => initialDrafts(summary.files));

  // Only a Calendar the caller may write Events to is a valid "existing
  // calendar" import target (#111, ADR-0034) — a Subscribed Calendar
  // (written only by Refresh's bypass, #84, ADR-0032) and a Calendar the
  // caller has no more than Viewer Access to both fall out of this.
  const writableCalendars = calendars.filter((calendar) => canWriteCalendarEvents(calendar));

  const totals = summarizeImport(summary);
  const summaryLine = formatImportSummaryLine(totals);
  const showDetails = hasAnyImportDetails(summary);

  function updateDraft(index: number, changes: Partial<EntryDraft>) {
    setDrafts((current) => current.map((draft, i) => (i === index ? { ...draft, ...changes } : draft)));
  }

  const canConfirm =
    drafts.some((draft) => draft.action !== "skip") &&
    drafts.every((draft) => draft.action !== "new" || draft.name.trim() !== "") &&
    drafts.every((draft) => draft.action !== "existing" || draft.calendarId !== "");

  function handleConfirm() {
    if (!canConfirm) return;

    const targets: ImportTarget[] = drafts.map((draft) => {
      if (draft.action === "skip") return { filename: draft.filename, action: "skip" };
      if (draft.action === "existing") {
        return { filename: draft.filename, action: "existing", calendarId: draft.calendarId };
      }
      return { filename: draft.filename, action: "new", name: draft.name.trim() };
    });
    onConfirm(targets);
  }

  return (
    <Dialog.Root
      open
      onOpenChange={(open) => {
        if (!open && !isSubmitting) onClose();
      }}
    >
      <Dialog.Portal>
        <Dialog.Backdrop className="fixed inset-0 z-40 bg-ink/20" />
        <Dialog.Popup className="fixed top-1/2 left-1/2 z-50 max-h-[85vh] w-[32rem] -translate-x-1/2 -translate-y-1/2 overflow-y-auto rounded-shell-lg bg-surface p-5 shadow-elevation-3">
          <Dialog.Title className="text-heading font-medium text-ink">Import preview</Dialog.Title>
          <p className="mt-1 text-label-sm text-ink-muted">{file.name}</p>

          <p className="mt-4 text-body text-ink">{summaryLine}</p>
          <p className="mt-1 text-label-sm text-ink-muted">
            Reminders: {formatReminderLine(totals.reminders)}
          </p>

          {showDetails && (
            <details className="mt-2">
              <summary className="cursor-pointer text-label-sm text-accent">Details</summary>
              <div className="mt-2">
                <ImportSummaryDetails summary={summary} showFilenames={isZip} />
              </div>
            </details>
          )}

          <div className="mt-4 flex flex-col gap-3">
            {summary.files.map((entrySummary, index) => {
              const draft = drafts[index];
              return (
                <div
                  key={entrySummary.filename}
                  className="rounded-shell-md border border-border p-3"
                >
                  <div className="flex items-center justify-between gap-2">
                    <p className="truncate text-body text-ink">{entrySummary.filename}</p>
                    <p className="shrink-0 text-label-sm text-ink-muted">
                      {entrySummary.eventCount.toLocaleString()} event
                      {entrySummary.eventCount === 1 ? "" : "s"} · {formatRange(entrySummary)}
                    </p>
                  </div>

                  {isZip ? (
                    <div className="mt-2 flex items-center gap-2">
                      <Checkbox
                        checked={draft.action !== "skip"}
                        onCheckedChange={(checked) =>
                          updateDraft(index, { action: checked ? "new" : "skip" })
                        }
                        aria-label={`Include ${entrySummary.filename}`}
                      />
                      <Input
                        value={draft.name}
                        onChange={(domEvent) => updateDraft(index, { name: domEvent.target.value })}
                        disabled={draft.action === "skip"}
                        aria-label={`Calendar name for ${entrySummary.filename}`}
                        className="flex-1"
                      />
                    </div>
                  ) : (
                    <div className="mt-2 flex flex-col gap-2">
                      <Select
                        aria-label="Import target"
                        value={draft.action === "existing" ? draft.calendarId : NEW_CALENDAR_VALUE}
                        onValueChange={(value) =>
                          value === NEW_CALENDAR_VALUE
                            ? updateDraft(index, { action: "new", calendarId: "" })
                            : updateDraft(index, { action: "existing", calendarId: value })
                        }
                        options={[
                          { value: NEW_CALENDAR_VALUE, label: "New calendar" },
                          ...writableCalendars.map((calendar) => ({
                            value: calendar.id,
                            label: calendarPickerLabel(calendar),
                          })),
                        ]}
                      />
                      {draft.action === "new" && (
                        <Input
                          value={draft.name}
                          onChange={(domEvent) => updateDraft(index, { name: domEvent.target.value })}
                          aria-label="New calendar name"
                        />
                      )}
                    </div>
                  )}
                </div>
              );
            })}
          </div>

          <div className="mt-5 flex justify-end gap-2">
            <Dialog.Close
              className={buttonClasses({ variant: "outline", color: "secondary", size: "small" })}
              disabled={isSubmitting}
            >
              Cancel
            </Dialog.Close>
            <Button
              size="small"
              disabled={!canConfirm}
              loading={isSubmitting}
              onClick={handleConfirm}
            >
              Import
            </Button>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
