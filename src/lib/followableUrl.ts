// The Event URL's render-time safety gate (ADR-0063). The value is stored
// verbatim and never validated on write, so it may hold anything — a
// half-typed string, Apple Calendar's message:// links, a javascript: payload
// on a shared Calendar. Whether it is followable is derived here, from the
// value alone, every render: never stored, never a second persisted flag.
//
// Returns value unchanged when its scheme is http/https, so an <a href> can
// point straight at it; undefined otherwise, which callers must render as
// plain text with no control rather than a control that does nothing.
export function followableUrl(value: string | undefined): string | undefined {
  if (!value) return undefined;

  let parsed: URL;
  try {
    parsed = new URL(value);
  } catch {
    return undefined;
  }

  return parsed.protocol === "http:" || parsed.protocol === "https:" ? value : undefined;
}
