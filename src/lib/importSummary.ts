import type { ImportSummary } from "./importApi";

export interface ImportTotals {
  eventCount: number;
  skippedCount: number;
  adjustedCount: number;
  ignoredCount: number;
  reminders: { notification: number; email: number };
}

/** Totals across every file in one import operation — a .zip's entries all
 * collapse into the same one-line summary (issue #79). */
export function summarizeImport(summary: ImportSummary): ImportTotals {
  const totals: ImportTotals = {
    eventCount: 0,
    skippedCount: 0,
    adjustedCount: 0,
    ignoredCount: 0,
    reminders: { notification: 0, email: 0 },
  };

  for (const file of summary.files) {
    totals.eventCount += file.eventCount;
    totals.skippedCount += file.skipped.reduce((sum, group) => sum + group.count, 0);
    totals.adjustedCount += file.adjusted.reduce((sum, group) => sum + group.count, 0);
    totals.ignoredCount += file.ignored.vtodo + file.ignored.vjournal + file.ignored.vfreebusy;
    totals.reminders.notification += file.reminders.notification;
    totals.reminders.email += file.reminders.email;
  }

  return totals;
}

/** The Import summary's one-line total, e.g.
 * "1,847 events · 12 skipped · 40 adjusted · 3 ignored" — zero categories
 * are left out rather than shown as "0 skipped". */
export function formatImportSummaryLine(totals: ImportTotals): string {
  const parts = [`${totals.eventCount.toLocaleString()} event${totals.eventCount === 1 ? "" : "s"}`];
  if (totals.skippedCount > 0) parts.push(`${totals.skippedCount.toLocaleString()} skipped`);
  if (totals.adjustedCount > 0) parts.push(`${totals.adjustedCount.toLocaleString()} adjusted`);
  if (totals.ignoredCount > 0) parts.push(`${totals.ignoredCount.toLocaleString()} ignored`);
  return parts.join(" · ");
}

/** Reminder counts by Channel, e.g. "4 notifications · 1 email" — kept
 * visible outside the details disclosure (and folded into the confirm
 * toast), since an incoming pile of email reminders needs to be seen before
 * confirming (issue #79). */
export function formatReminderLine(reminders: ImportTotals["reminders"]): string {
  return `${reminders.notification.toLocaleString()} notification${reminders.notification === 1 ? "" : "s"} · ${reminders.email.toLocaleString()} email${reminders.email === 1 ? "" : "s"}`;
}
