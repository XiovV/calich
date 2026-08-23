import type { WorkspaceMember } from "../lib/workspaceMembersApi";

// computePickableUsers is the invite picker's User pool: every Member of
// the Workspace minus whoever's already an Attendee (invited or staged) and
// minus the signed-in caller — an Organizer or Attendee can't invite
// themselves, they're already on the Event.
export function computePickableUsers(
  availableUsers: WorkspaceMember[],
  invitedUserIds: Set<number | null>,
  stagedUserIds: Set<number>,
  currentUserId: number | undefined,
): WorkspaceMember[] {
  return availableUsers.filter(
    (user) =>
      user.userId !== currentUserId &&
      !invitedUserIds.has(user.userId) &&
      !stagedUserIds.has(user.userId),
  );
}
