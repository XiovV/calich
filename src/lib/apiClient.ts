export class ApiError extends Error {
  code: string;
  status: number;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = "ApiError";
    this.code = code;
    this.status = status;
  }
}

export async function errorFromResponse(response: Response): Promise<ApiError> {
  let code = "unknown_error";
  let message = "Something went wrong.";
  try {
    const body = (await response.json()) as { error?: { code?: string; message?: string } };
    if (body.error?.code) code = body.error.code;
    if (body.error?.message) message = body.error.message;
  } catch {
    // Body wasn't JSON (or was empty) — fall back to the generic message above.
  }
  return new ApiError(response.status, code, message);
}

function authHeader(accessToken: string): HeadersInit {
  return { Authorization: `Bearer ${accessToken}` };
}

// sessionRefresher is registered by authStore at module init, never imported
// here directly — apiClient must stay a leaf module (it imports nothing),
// since authStore -> authApi -> apiClient already forms a chain and reaching
// back in would close it into a cycle ESM tolerates in Vite but chokes on in
// Vitest. It performs the refresh, writes the new token into the store, and
// returns it; on failure it puts the store into "unauthenticated" and rejects.
type SessionRefresher = () => Promise<string>;

let sessionRefresher: SessionRefresher | null = null;

export function setSessionRefresher(refresher: SessionRefresher): void {
  sessionRefresher = refresher;
}

// Shared in-flight refresh so a burst of concurrent 401s produces one
// refresh, not a stampede.
let inFlightRefresh: Promise<string> | null = null;

function refreshSession(): Promise<string> {
  if (!sessionRefresher) return Promise.reject(new Error("No session refresher registered."));

  if (!inFlightRefresh) {
    inFlightRefresh = sessionRefresher().finally(() => {
      inFlightRefresh = null;
    });
  }
  return inFlightRefresh;
}

function withToken(init: RequestInit, accessToken: string): RequestInit {
  return { ...init, headers: { ...init.headers, ...authHeader(accessToken) } };
}

// authApi.login and authApi.refresh call fetch directly instead of this — a
// 401 from either is the real answer, not something to recover from. Every
// other module routes through here so an expired Access token (15-minute
// TTL) never surfaces as a user-visible failure. A recurring 401 on the
// retry, or a failed refresh, is returned untouched for the caller's
// existing `if (!response.ok) throw ...` handling.
//
// shouldRefreshOn401 defaults to always-refresh; pass one for an endpoint
// where the HTTP status alone can't tell an expired token from a semantic
// 401 answer — e.g. authApi.changePassword (#249), whose 401 is
// RequireAuth's "unauthorized" for an expired token (refreshable, same as
// everywhere else) on every route it guards, but also the handler's own
// "invalid_credentials" for a wrong current password (final — retrying it
// as a token refresh doubles the bcrypt compare and rotates the refresh
// token for nothing, on a path an ordinary typo hits). Both share the same
// status code, so the predicate is handed a clone of the response to
// inspect the body instead — the original's body is left untouched for
// whichever fetch's result the caller ultimately reads.
export async function authedFetch(
  accessToken: string,
  input: string,
  init: RequestInit = {},
  { shouldRefreshOn401 }: { shouldRefreshOn401?: (response: Response) => Promise<boolean> } = {},
): Promise<Response> {
  const response = await fetch(input, withToken(init, accessToken));
  if (response.status !== 401) return response;
  if (shouldRefreshOn401 && !(await shouldRefreshOn401(response.clone()))) return response;

  let refreshedToken: string;
  try {
    refreshedToken = await refreshSession();
  } catch {
    return response;
  }

  return fetch(input, withToken(init, refreshedToken));
}
