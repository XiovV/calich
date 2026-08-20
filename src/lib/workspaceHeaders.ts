import { requireActiveWorkspaceId } from "./workspacesStore";

// workspaceHeaders is how a request asserts which Workspace it is acting in
// (#155, ADR-0045). There is no server-side notion of a "currently active"
// Workspace beyond what the client claims per request, so every
// Workspace-scoped call must carry this header — one that omits it is
// refused as a non-Member before its handler ever runs, a 403 that says
// nothing about what the call was trying to do (#225).
//
// extra merges on top, so a call with a JSON body writes
// workspaceHeaders({ "Content-Type": "application/json" }). Deliberately
// narrower than HeadersInit: a Headers instance or an entry-pair array would
// spread to nothing here, silently dropping the caller's headers.
export function workspaceHeaders(
  extra?: Record<string, string>,
): Record<string, string> {
  return { "X-Workspace-Id": String(requireActiveWorkspaceId()), ...extra };
}
