import { downloadBlob } from "./downloadBlob";

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
    scope: { type: "all" } | { type: "occurrence"; occurrenceStart: Date } = {
      type: "all",
    },
  ): Promise<void> {
    let url = `/api/events/${eventId}/ics`;
    if (scope.type === "occurrence") {
      const params = new URLSearchParams({
        scope: "occurrence",
        occurrenceStart: scope.occurrenceStart.toISOString(),
      });
      url += `?${params.toString()}`;
    }
    await downloadBlob(accessToken, url, `${sanitizeFilename(title, "event")}.ics`);
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
};
