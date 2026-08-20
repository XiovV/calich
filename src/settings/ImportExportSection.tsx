import { useRef, useState, type ChangeEvent, type DragEvent } from "react";
import { Download, Upload } from "lucide-react";
import { Button } from "../components/ui/Button";
import { useAuthStore } from "../lib/authStore";
import { useCalendarsStore } from "../lib/calendarsStore";
import { useEventsStore } from "../lib/eventsStore";
import { errorMessage } from "../lib/errorMessage";
import { icsApi, type ExportSummary } from "../lib/icsApi";
import {
  importApi,
  type ImportSummary,
  type ImportTarget,
} from "../lib/importApi";
import {
  formatImportSummaryLine,
  formatReminderLine,
  hasAnyImportDetails,
  summarizeImport,
} from "../lib/importSummary";
import { toast } from "../lib/toast";
import { readZipEntryNames } from "../lib/zipEntryNames";
import { ExportSummaryDialog } from "../components/ExportSummaryDialog";
import { ImportPreviewDialog } from "./ImportPreviewDialog";
import { ImportSummaryDetails } from "./ImportSummaryDetails";

interface PendingImport {
  file: File;
  isZip: boolean;
  summary: ImportSummary;
}

/** The Import summary of the import that just ran, kept on the page after
 * the dialog closes. The toast states the totals and is gone in seconds;
 * what an import skipped, and why, is the whole point of the summary
 * (ADR-0030), so it stays until the next import replaces it (#228). */
interface CompletedImport {
  isZip: boolean;
  summary: ImportSummary;
}

function isZipFile(file: File): boolean {
  return file.name.toLowerCase().endsWith(".zip");
}

function isSupportedFile(file: File): boolean {
  const lower = file.name.toLowerCase();
  return lower.endsWith(".ics") || lower.endsWith(".zip");
}

/**
 * The Settings page's Import & export section (#79, ADR-0030): exports
 * every owned Calendar as a .zip, and imports a single .ics or .zip file
 * through a dryRun preview before writing anything. Subscribed Calendars
 * are excluded from the export — a frozen snapshot is the wrong artifact
 * for something a Refresh will overwrite anyway — so the button is labelled
 * "Download my calendars" rather than "all calendars" to disclose that scope
 * (#90, ADR-0032). Their Subscription URLs are listed in the archive's
 * subscriptions.txt instead, since that's what actually restores them
 * elsewhere.
 */
export function ImportExportSection() {
  const accessToken = useAuthStore((state) => state.accessToken);
  const calendars = useCalendarsStore((state) => state.calendars);
  const fetchCalendars = useCalendarsStore((state) => state.fetchCalendars);
  const fetchEvents = useEventsStore((state) => state.fetchEvents);

  const fileInputRef = useRef<HTMLInputElement>(null);
  const [isDraggingOver, setIsDraggingOver] = useState(false);
  const [isExporting, setIsExporting] = useState(false);
  const [isPreviewing, setIsPreviewing] = useState(false);
  const [isImporting, setIsImporting] = useState(false);
  const [completedImport, setCompletedImport] =
    useState<CompletedImport | null>(null);
  const [pendingImport, setPendingImport] = useState<PendingImport | null>(
    null,
  );
  const [pendingExportSummary, setPendingExportSummary] =
    useState<ExportSummary | null>(null);

  async function downloadAllCalendars() {
    if (!accessToken) return;
    try {
      await icsApi.downloadAllCalendars(accessToken);
    } catch (err) {
      toast.error(errorMessage(err));
    }
  }

  // The Export summary pre-flight (#134, ADR-0041): nothing oversized keeps
  // this a single click, exactly like before.
  async function handleExportAll() {
    if (!accessToken || isExporting) return;

    setIsExporting(true);
    try {
      const summary =
        await icsApi.allCalendarsOversizedAttachments(accessToken);
      if (summary.count === 0) {
        await downloadAllCalendars();
      } else {
        setPendingExportSummary(summary);
      }
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setIsExporting(false);
    }
  }

  async function handleConfirmExport() {
    setIsExporting(true);
    try {
      await downloadAllCalendars();
    } finally {
      setIsExporting(false);
      setPendingExportSummary(null);
    }
  }

  async function previewFile(file: File) {
    if (!accessToken || isPreviewing) return;

    if (!isSupportedFile(file)) {
      toast.error("Select a .ics or .zip file.");
      return;
    }

    setIsPreviewing(true);
    try {
      const isZip = isZipFile(file);
      const filenames = isZip ? await readZipEntryNames(file) : [file.name];
      if (filenames.length === 0) {
        toast.error("This zip has no .ics files in it.");
        return;
      }

      const targets: ImportTarget[] = filenames.map((filename) => ({
        filename,
        action: "new",
      }));
      const summary = await importApi.preview(accessToken, file, targets);
      setPendingImport({ file, isZip, summary });
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setIsPreviewing(false);
    }
  }

  // A FileList is live, so the picker's files have to be copied out before
  // the input is reset — clearing the value empties the very list a captured
  // reference points at, which left every Choose file import silently doing
  // nothing (#226). The reset itself has to stay: without it, picking the
  // same file twice in a row fires no second change event.
  function handleInputChange(domEvent: ChangeEvent<HTMLInputElement>) {
    const files = Array.from(domEvent.target.files ?? []);
    domEvent.target.value = "";
    handleFiles(files);
  }

  function handleDrop(domEvent: DragEvent<HTMLDivElement>) {
    domEvent.preventDefault();
    setIsDraggingOver(false);
    handleFiles(Array.from(domEvent.dataTransfer.files));
  }

  function handleFiles(files: File[]) {
    if (files.length === 0) return;
    if (files.length > 1) {
      toast.error("Select one file at a time.");
      return;
    }
    previewFile(files[0]);
  }

  async function handleConfirmImport(targets: ImportTarget[]) {
    if (!accessToken || !pendingImport) return;

    setIsImporting(true);
    try {
      const summary = await importApi.commit(
        accessToken,
        pendingImport.file,
        targets,
      );
      await Promise.all([fetchCalendars(), fetchEvents()]);
      setCompletedImport({ isZip: pendingImport.isZip, summary });
      setPendingImport(null);
      const totals = summarizeImport(summary);
      toast.success(
        `${formatImportSummaryLine(totals)} · ${formatReminderLine(totals.reminders)}`,
      );
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setIsImporting(false);
    }
  }

  return (
    <section>
      <h2 className="text-heading font-medium text-ink">Import & export</h2>
      <p className="mt-1 text-body text-ink-muted">
        Export every calendar you own as a .zip of .ics files, or import one
        from another app. Re-importing a file duplicates its events and
        attachments — delete the calendar it made to undo it.
      </p>

      <Button
        className="mt-4"
        variant="outline"
        leadingIcon={<Download className="size-4" />}
        loading={isExporting}
        onClick={handleExportAll}
      >
        Download my calendars (.zip)
      </Button>

      <div
        className={`mt-4 flex flex-col items-center gap-2 rounded-shell-md border border-dashed p-8 text-center transition-colors ${
          isDraggingOver ? "border-accent bg-surface-hover" : "border-border"
        }`}
        onDragOver={(domEvent) => {
          domEvent.preventDefault();
          setIsDraggingOver(true);
        }}
        onDragLeave={() => setIsDraggingOver(false)}
        onDrop={handleDrop}
      >
        <Upload className="size-6 text-ink-muted" />
        <p className="text-body text-ink">Drop a .ics or .zip file here</p>
        <Button
          type="button"
          size="small"
          variant="outline"
          loading={isPreviewing}
          onClick={() => fileInputRef.current?.click()}
        >
          Choose file
        </Button>
        <input
          ref={fileInputRef}
          type="file"
          accept=".ics,.zip"
          className="hidden"
          onChange={handleInputChange}
        />
      </div>

      {completedImport && hasAnyImportDetails(completedImport.summary) && (
        <div className="mt-4 rounded-shell-md border border-border p-4">
          <p className="text-label-sm font-medium text-ink">What the last import left out</p>
          <div className="mt-2">
            <ImportSummaryDetails
              summary={completedImport.summary}
              showFilenames={completedImport.isZip}
            />
          </div>
        </div>
      )}

      {pendingImport && (
        <ImportPreviewDialog
          file={pendingImport.file}
          isZip={pendingImport.isZip}
          summary={pendingImport.summary}
          calendars={calendars}
          isSubmitting={isImporting}
          onConfirm={handleConfirmImport}
          onClose={() => setPendingImport(null)}
        />
      )}
      {pendingExportSummary && (
        <ExportSummaryDialog
          summary={pendingExportSummary}
          isSubmitting={isExporting}
          onConfirm={handleConfirmExport}
          onClose={() => setPendingExportSummary(null)}
        />
      )}
    </section>
  );
}
