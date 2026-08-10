// Builds the accept-workspace-invite URL for token (ADR-0044) — this
// browser's own origin, not something the server tracks, mirroring the
// retired account-level buildInviteLink (ADR-0042).
export function buildWorkspaceInviteLink(token: string): string {
  return `${window.location.origin}/accept-workspace-invite?token=${encodeURIComponent(token)}`;
}
