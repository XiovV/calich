import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ImportSummary } from "../lib/importApi";

// Same convention as the other component tests: the *Api modules are mocked,
// the stores are real. What these cover is the wiring between picking a file
// and the dryRun preview request it has to produce — the flow that #226 found
// silently dead behind the Choose file button.
vi.mock("../lib/importApi", () => ({
  importApi: { preview: vi.fn(), commit: vi.fn() },
}));
vi.mock("../lib/icsApi", () => ({
  icsApi: {
    downloadAllCalendars: vi.fn(),
    allCalendarsOversizedAttachments: vi.fn(),
  },
}));
vi.mock("../lib/zipEntryNames", () => ({ readZipEntryNames: vi.fn() }));
vi.mock("../lib/toast", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

const { importApi } = await import("../lib/importApi");
const { readZipEntryNames } = await import("../lib/zipEntryNames");
const { toast } = await import("../lib/toast");
const { useAuthStore } = await import("../lib/authStore");
const { useCalendarsStore } = await import("../lib/calendarsStore");
const { useEventsStore } = await import("../lib/eventsStore");
const { useShellStore } = await import("../lib/shellStore");
const { ImportExportSection } = await import("./ImportExportSection");

function summaryFor(filename: string, calendarName: string): ImportSummary {
  return {
    files: [
      {
        filename,
        calendarName,
        eventCount: 3,
        skipped: [],
        adjusted: [],
        ignored: { vtodo: 0, vjournal: 0, vfreebusy: 0 },
        reminders: { notification: 0, email: 0 },
        attachments: { imported: 0, tooLarge: 0, tooMany: 0, ignoredUri: 0 },
      },
    ],
  };
}

function icsFile(name = "personal.ics") {
  return new File(["BEGIN:VCALENDAR\nEND:VCALENDAR\n"], name, {
    type: "text/calendar",
  });
}

/**
 * Picks `file` through the hidden file input, the way the OS picker does.
 *
 * jsdom models a file input's FileList exactly as a browser does — it is
 * live, so clearing the input's value empties the very list object a change
 * handler already captured, which is the whole of #226. Keeping that fidelity
 * means populating jsdom's own FileList (its wrapper is a read-only proxy,
 * hence the impl symbol) rather than handing the handler a detached array,
 * which would quietly pass whether the bug is present or not.
 */
function chooseFile(input: HTMLInputElement, file: File) {
  const list = input.files!;
  const implSymbol = Object.getOwnPropertySymbols(list).find(
    (symbol) => symbol.toString() === "Symbol(impl)",
  );
  if (!implSymbol) {
    throw new Error(
      "No Symbol(impl) on the input's FileList — jsdom's internals moved, and this helper can no longer populate a live list.",
    );
  }
  const entries = (list as unknown as Record<symbol, File[]>)[implSymbol];
  entries.length = 0;
  entries.push(file);
  fireEvent.change(input);
}

function fileInput(): HTMLInputElement {
  return document.querySelector<HTMLInputElement>('input[type="file"]')!;
}

function dropZone() {
  return screen.getByText("Drop a .ics or .zip file here").parentElement!;
}

beforeEach(() => {
  vi.clearAllMocks();
  useAuthStore.setState({ status: "authenticated", accessToken: "token-123" });
  useCalendarsStore.setState({ calendars: [], fetchCalendars: vi.fn() });
  useEventsStore.setState({ fetchEvents: vi.fn() });
});

describe("ImportExportSection", () => {
  it("previews a .ics picked through Choose file, and imports it on confirm", async () => {
    vi.mocked(importApi.preview).mockResolvedValue(
      summaryFor("personal.ics", "Personal"),
    );
    vi.mocked(importApi.commit).mockResolvedValue(
      summaryFor("personal.ics", "Personal"),
    );
    render(<ImportExportSection />);

    const file = icsFile();
    chooseFile(fileInput(), file);

    await screen.findByRole("dialog", { name: "Import preview" });
    expect(importApi.preview).toHaveBeenCalledWith("token-123", file, [
      { filename: "personal.ics", action: "new" },
    ]);

    await userEvent.click(screen.getByRole("button", { name: "Import" }));

    await waitFor(() =>
      expect(importApi.commit).toHaveBeenCalledWith("token-123", file, [
        { filename: "personal.ics", action: "new", name: "Personal" },
      ]),
    );
  });

  // The Import summary is a user's only view of a lossy translation
  // (ADR-0030), so what an import left out has to survive the moment the
  // dialog closes — the toast's counts alone can't say why (#228).
  it("states why an event was skipped in the post-import result", async () => {
    const withSkip = summaryFor("personal.ics", "Personal");
    withSkip.files[0].skipped = [
      { reason: "end before start", count: 1, samples: ["Backwards"] },
    ];
    withSkip.files[0].adjusted = [
      { reason: "zero-length event given a 30-minute duration", count: 1 },
    ];
    vi.mocked(importApi.preview).mockResolvedValue(withSkip);
    vi.mocked(importApi.commit).mockResolvedValue(withSkip);
    render(<ImportExportSection />);

    chooseFile(fileInput(), icsFile());
    await screen.findByRole("dialog", { name: "Import preview" });
    await userEvent.click(screen.getByRole("button", { name: "Import" }));

    await waitFor(() =>
      expect(
        screen.getByText(/Skipped 1 — end before start \(Backwards\)/),
      ).toBeInTheDocument(),
    );
    expect(
      screen.getByText(
        /Adjusted 1 — zero-length event given a 30-minute duration/,
      ),
    ).toBeInTheDocument();
  });

  it("previews a .zip picked through Choose file", async () => {
    vi.mocked(readZipEntryNames).mockResolvedValue(["work.ics"]);
    vi.mocked(importApi.preview).mockResolvedValue(summaryFor("work.ics", "Work"));
    render(<ImportExportSection />);

    const file = new File(["PK"], "calendars.zip", { type: "application/zip" });
    chooseFile(fileInput(), file);

    await screen.findByRole("dialog", { name: "Import preview" });
    expect(importApi.preview).toHaveBeenCalledWith("token-123", file, [
      { filename: "work.ics", action: "new" },
    ]);
  });

  // The input is reset so that picking the same file twice still fires a
  // change event the second time — just not before its files have been read.
  it("clears the input after reading it, and previews the same file twice", async () => {
    vi.mocked(importApi.preview).mockResolvedValue(
      summaryFor("personal.ics", "Personal"),
    );
    render(<ImportExportSection />);

    chooseFile(fileInput(), icsFile());
    expect(fileInput().value).toBe("");

    await userEvent.click(await screen.findByRole("button", { name: "Cancel" }));

    chooseFile(fileInput(), icsFile());

    await waitFor(() => expect(importApi.preview).toHaveBeenCalledTimes(2));
  });

  it("rejects an unsupported file type", async () => {
    render(<ImportExportSection />);

    chooseFile(fileInput(), new File(["nope"], "notes.txt", { type: "text/plain" }));

    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith("Select a .ics or .zip file."),
    );
    expect(importApi.preview).not.toHaveBeenCalled();
  });

  // The import created a new Calendar server-side; fetchCalendars is the
  // only way this session learns its id. Without a reconcile after it, the
  // new Calendar's toggle stayed unset until a reload (#229).
  it("checks the imported calendar's toggle immediately, without a reload", async () => {
    const created = {
      id: "cal-new",
      name: "Personal",
      color: "peacock" as const,
      isOwner: true,
      access: "owner" as const,
    };
    useCalendarsStore.setState({
      calendars: [],
      fetchCalendars: vi.fn(async () => {
        useCalendarsStore.setState({ calendars: [created] });
      }),
    });
    vi.mocked(importApi.preview).mockResolvedValue(
      summaryFor("personal.ics", "Personal"),
    );
    vi.mocked(importApi.commit).mockResolvedValue(
      summaryFor("personal.ics", "Personal"),
    );
    render(<ImportExportSection />);

    chooseFile(fileInput(), icsFile());
    await screen.findByRole("dialog", { name: "Import preview" });
    await userEvent.click(screen.getByRole("button", { name: "Import" }));

    await waitFor(() =>
      expect(useShellStore.getState().checkedCalendarIds.has("cal-new")).toBe(
        true,
      ),
    );
    expect(useShellStore.getState().knownCalendarIds.has("cal-new")).toBe(
      true,
    );
  });

  // Importing more events into a Calendar that already exists must not
  // touch its current toggle — a Calendar the caller had deliberately
  // hidden stays hidden after importing into it (#229).
  it("leaves an existing calendar's toggle alone when importing into it", async () => {
    const existing = {
      id: "cal-existing",
      name: "Personal",
      color: "peacock" as const,
      isOwner: true,
      access: "owner" as const,
    };
    useCalendarsStore.setState({
      calendars: [existing],
      fetchCalendars: vi.fn(async () => {
        useCalendarsStore.setState({ calendars: [existing] });
      }),
    });
    useShellStore.setState({
      checkedCalendarIds: new Set(),
      knownCalendarIds: new Set(["cal-existing"]),
    });
    vi.mocked(importApi.preview).mockResolvedValue(
      summaryFor("personal.ics", "Personal"),
    );
    vi.mocked(importApi.commit).mockResolvedValue(
      summaryFor("personal.ics", "Personal"),
    );
    render(<ImportExportSection />);

    chooseFile(fileInput(), icsFile());
    await screen.findByRole("dialog", { name: "Import preview" });
    await userEvent.click(screen.getByRole("button", { name: "Import" }));

    await waitFor(() => expect(importApi.commit).toHaveBeenCalled());
    expect(
      useShellStore.getState().checkedCalendarIds.has("cal-existing"),
    ).toBe(false);
  });

  it("previews a .ics dropped on the drop zone", async () => {
    vi.mocked(importApi.preview).mockResolvedValue(
      summaryFor("personal.ics", "Personal"),
    );
    render(<ImportExportSection />);

    const file = icsFile();
    fireEvent.drop(dropZone(), { dataTransfer: { files: [file] } });

    await screen.findByRole("dialog", { name: "Import preview" });
    expect(importApi.preview).toHaveBeenCalledWith("token-123", file, [
      { filename: "personal.ics", action: "new" },
    ]);
  });
});
