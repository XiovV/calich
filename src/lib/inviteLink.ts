// Builds the accept-invite URL for token (ADR-0042). Built client-side —
// this browser's own origin, not something the server tracks — and sent
// verbatim to accountsApi.sendInviteEmail when emailing it.
export function buildInviteLink(token: string): string {
  return `${window.location.origin}/accept-invite?token=${encodeURIComponent(token)}`;
}
