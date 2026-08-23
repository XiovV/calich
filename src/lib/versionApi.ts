import { errorFromResponse } from "./apiClient";

// The instance's build label (#256, ADR-0072). Public and unauthenticated,
// so this uses a bare fetch rather than authedFetch — there is no token to
// send and no 401 to refresh through.
//
// The label is opaque: never parsed, compared, or `v`-prefixed here. It is
// rendered exactly as the linker wrote it, so a tag of "1.2.3" shows as
// "1.2.3" and one of "v1.2.3" shows as "v1.2.3".
async function fetchVersion(): Promise<string> {
  const response = await fetch("/api/version");
  if (!response.ok) throw await errorFromResponse(response);

  const body = (await response.json()) as { version: string };
  return body.version;
}

// The label is fixed for the lifetime of the served binary, so the request
// is made once per page load and the promise is shared by every caller.
// Memoised at module scope rather than inside the hook so that unmounting
// and remounting the top bar re-reads the same answer instead of refetching.
//
// A rejection is cached too, deliberately: a failed lookup stays failed for
// the rest of the page's life rather than retrying on every remount. The
// badge is ambient decoration, and decoration does not get to generate
// traffic.
let inFlight: Promise<string> | null = null;

export function getVersion(): Promise<string> {
  if (!inFlight) inFlight = fetchVersion();
  return inFlight;
}
