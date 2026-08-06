import { useRef, useState, type ChangeEvent, type DragEvent } from "react";
import { Download, Upload } from "lucide-react";
import { Button } from "../components/ui/Button";
import { useAuthStore } from "../lib/authStore";
import { useCalendarsStore } from "../lib/calendarsStore";
import { useEventsStore } from "../lib/eventsStore";
import { errorMessage } from "../lib/errorMessage";
import { icsApi } from "../lib/icsApi";
import { importApi, type ImportSummary, type ImportTarget } from "../lib/importApi";
import { formatImportSummaryLine, formatReminderLine, summarizeImport } from "../lib/importSummary";
import { toast } from "../lib/toast";
import { readZipEntryNames } from "../lib/zipEntryNames";
import { ImportPreviewDialog } from "./ImportPreviewDialog";

interface PendingImport {
  file: File;
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
 * The Settings page's Import & export section (#79, ADR-0030): exports every
 * Calendar as a .zip, and imports a single .ics or .zip file through a
 * dryRun preview before writing anything.
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
  const [pendingImport, setPendingImport] = useState<PendingImport | null>(null);

  async function handleExportAll() {
    if (!accessToken || isExporting) return;

    setIsExporting(true);
    try {
      await icsApi.downloadAllCalendars(accessToken);
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setIsExporting(false);
    }
  }

  async function handleFile(file: File) {
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

      const targets: ImportTarget[] = filenames.map((filename) => ({ filename, action: "new" }));
      const summary = await importApi.preview(accessToken, file, targets);
      setPendingImport({ file, isZip, summary });
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setIsPreviewing(false);
    }
  }

  function handleInputChange(domEvent: ChangeEvent<HTMLInputElement>) {
    const files = domEvent.target.files;
    domEvent.target.value = "";
    if (files) handleFileList(files);
  }

  function handleDrop(domEvent: DragEvent<HTMLDivElement>) {
    domEvent.preventDefault();
    setIsDraggingOver(false);
    handleFileList(domEvent.dataTransfer.files);
  }

  function handleFileList(files: FileList) {
    if (files.length === 0) return;
    if (files.length > 1) {
      toast.error("Select one file at a time.");
      return;
    }
    handleFile(files[0]);
  }

  async function handleConfirmImport(targets: ImportTarget[]) {
    if (!accessToken || !pendingImport) return;

    setIsImporting(true);
    try {
      const summary = await importApi.commit(accessToken, pendingImport.file, targets);
      await Promise.all([fetchCalendars(), fetchEvents()]);
      setPendingImport(null);
      const totals = summarizeImport(summary);
      toast.success(`${formatImportSummaryLine(totals)} · ${formatReminderLine(totals.reminders)}`);
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setIsImporting(false);
    }
  }

  return (
    <section className="mt-8">
      <h2 className="text-heading font-medium text-ink">Import & export</h2>
      <p className="mt-1 text-body text-ink-muted">
        Export every calendar as a .zip of .ics files, or import a .ics or .zip export from
        another app. Importing the same file twice creates duplicate events — if an import goes
        wrong, undo it by deleting the calendar it created.
      </p>

      <Button
        className="mt-4"
        variant="outline"
        leadingIcon={<Download className="size-4" />}
        loading={isExporting}
        onClick={handleExportAll}
      >
        Export all calendars (.zip)
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
    </section>
  );
}
