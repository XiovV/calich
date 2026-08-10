import { useEffect, useMemo, useState } from "react";
import { Trash2 } from "lucide-react";
import { attendeesApi, type Attendee, type AttendeeResponse } from "../lib/attendeesApi";
import { groupsApi, type Group } from "../lib/groupsApi";
import { workspaceMembersApi, type WorkspaceMember } from "../lib/workspaceMembersApi";
import { useAuthStore } from "../lib/authStore";
import { errorMessage } from "../lib/errorMessage";
import { toast } from "../lib/toast";
import { Button } from "../components/ui/Button";
import { IconButton } from "../components/ui/IconButton";
import { Select } from "../components/ui/Select";
import { fieldLabelClass } from "../components/ui/fieldStyles";

const RESPONSE_LABEL: Record<AttendeeResponse, string> = {
  "needs-action": "Needs action",
  accepted: "Accepted",
  declined: "Declined",
  tentative: "Tentative",
};

// TargetKey encodes the invite picker's entries as "user:<id>" or
// "group:<id>" so one Select can offer both, mirroring
// CalendarSharingSection's own TargetKey (#159) — minus a Role, since an
// Attendee has none.
type TargetKey = `user:${number}` | `group:${number}`;

function userTargetKey(userId: number): TargetKey {
  return `user:${userId}`;
}
function groupTargetKey(groupId: number): TargetKey {
  return `group:${groupId}`;
}

interface EventAttendeesSectionProps {
  // The Master Event Attendees are invited to (ADR-0046) — undefined in
  // create mode, where there's no id yet to invite anyone to; an Override
  // never carries its own, mirroring Attachments' own Master-scoping.
  eventId: string | undefined;
  // Whether the caller may invite/remove Attendees — an Editor or Owner of
  // the Event's Calendar, the same gate the backend's
  // attendeeManagementCalendar enforces. False renders the list read-only.
  canManage: boolean;
}

// EventAttendeesSection is the Attendee surface inside EventModal (#168,
// ADR-0046): who's invited and their Response, an invite picker (User or
// Group, scoped to the Event's Calendar's Workspace) for whoever can manage
// the Event, and — independent of canManage — the signed-in caller's own
// Accept/Decline/Tentative when they're one of the Attendees. Renders
// nothing in create mode (eventId undefined) or once loaded, canManage is
// false and there's nobody invited to show.
export function EventAttendeesSection({ eventId, canManage }: EventAttendeesSectionProps) {
  const accessToken = useAuthStore((state) => state.accessToken);
  const currentUserId = useAuthStore((state) => state.user?.id);

  const [attendees, setAttendees] = useState<Attendee[] | null>(null);
  const [availableUsers, setAvailableUsers] = useState<WorkspaceMember[]>([]);
  const [availableGroups, setAvailableGroups] = useState<Group[]>([]);
  const [selectedTarget, setSelectedTarget] = useState<TargetKey | "">("");
  const [isInviting, setIsInviting] = useState(false);
  const [inviteError, setInviteError] = useState<string | null>(null);
  const [removingUserId, setRemovingUserId] = useState<number | null>(null);
  const [isRespondingTo, setIsRespondingTo] = useState<AttendeeResponse | null>(null);

  useEffect(() => {
    if (!accessToken || !eventId) return;
    let cancelled = false;
    (async () => {
      try {
        const fetchedAttendees = await attendeesApi.list(accessToken, eventId);
        if (cancelled) return;
        setAttendees(fetchedAttendees);
      } catch {
        if (!cancelled) toast.error("Failed to load attendees.");
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [accessToken, eventId]);

  useEffect(() => {
    if (!accessToken || !eventId || !canManage) return;
    let cancelled = false;
    (async () => {
      try {
        // workspaceMembersApi/groupsApi are scoped to the currently active
        // Workspace, not eventId's Calendar's own Workspace by id — this is
        // only equivalent to "the Event's Calendar's Workspace" (the spec's
        // actual requirement) because canManage being true already implies
        // editedCalendar came from calendarsStore, which is itself always
        // scoped to the active Workspace (ADR-0045): a Calendar the caller
        // can write Events to is never in a Workspace other than the active
        // one. calendarsApi.shareTargets is the calendar-id-scoped
        // alternative CalendarSharingSection uses, but it's Owner-only —
        // it would 403 for an Editor who can manage Attendees but not
        // Sharing (attendeeManagementCalendar accepts Editor, not just
        // Owner), so it's the wrong fit here despite scoping more precisely.
        const [members, groups] = await Promise.all([
          workspaceMembersApi.list(accessToken),
          groupsApi.list(accessToken),
        ]);
        if (cancelled) return;
        setAvailableUsers(members);
        setAvailableGroups(groups);
      } catch {
        if (!cancelled) toast.error("Failed to load invite targets.");
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [accessToken, eventId, canManage]);

  const invitedUserIds = new Set((attendees ?? []).map((a) => a.userId));
  const pickableUsers = availableUsers.filter((user) => !invitedUserIds.has(user.userId));

  const targetOptions = useMemo(
    () => [
      ...pickableUsers.map((user) => ({
        value: userTargetKey(user.userId),
        label: user.name,
      })),
      ...availableGroups.map((group) => ({
        value: groupTargetKey(group.id),
        label: `${group.name} (group)`,
      })),
    ],
    [pickableUsers, availableGroups],
  );

  const effectiveTarget = targetOptions.some((option) => option.value === selectedTarget)
    ? selectedTarget
    : (targetOptions[0]?.value ?? "");

  async function handleInvite() {
    if (!accessToken || !eventId || !effectiveTarget) return;
    const [kind, idPart] = effectiveTarget.split(":");
    setIsInviting(true);
    setInviteError(null);
    try {
      if (kind === "user") {
        const invited = await attendeesApi.add(accessToken, eventId, Number(idPart));
        const user = pickableUsers.find((u) => u.userId === invited.userId);
        setAttendees((prev) => [...(prev ?? []), { ...invited, name: user?.name }]);
      } else {
        const invited = await attendeesApi.addGroup(accessToken, eventId, Number(idPart));
        const byId = new Map(availableUsers.map((u) => [u.userId, u.name]));
        setAttendees((prev) => [
          ...(prev ?? []),
          ...invited.map((a) => ({ ...a, name: byId.get(a.userId) })),
        ]);
      }
      setSelectedTarget("");
    } catch (err) {
      setInviteError(errorMessage(err));
    } finally {
      setIsInviting(false);
    }
  }

  async function handleRemove(attendee: Attendee) {
    if (!accessToken || !eventId) return;
    setRemovingUserId(attendee.userId);
    const previous = attendees;
    setAttendees((prev) => (prev ?? []).filter((a) => a.userId !== attendee.userId));
    try {
      await attendeesApi.remove(accessToken, eventId, attendee.userId);
    } catch {
      setAttendees(previous);
      toast.error(`Failed to remove ${attendee.name ?? "attendee"}.`);
    } finally {
      setRemovingUserId(null);
    }
  }

  async function handleRespond(response: AttendeeResponse) {
    if (!accessToken || !eventId) return;
    setIsRespondingTo(response);
    try {
      const updated = await attendeesApi.setResponse(accessToken, eventId, response);
      setAttendees((prev) =>
        (prev ?? []).map((a) => (a.userId === updated.userId ? { ...a, ...updated } : a)),
      );
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setIsRespondingTo(null);
    }
  }

  if (!eventId) return null;

  const loading = attendees === null;
  const ownAttendee = attendees?.find((a) => a.userId === currentUserId);

  if (!loading && !canManage && attendees.length === 0) return null;

  return (
    <div className="mt-4 border-t border-border pt-4">
      <p className={fieldLabelClass}>Attendees</p>

      {loading ? (
        <p className="mt-1.5 text-label-sm text-ink-muted">Loading…</p>
      ) : (
        <>
          {attendees.length > 0 ? (
            <ul className="mt-2 flex max-h-40 flex-col gap-1.5 overflow-y-auto">
              {attendees.map((attendee) => (
                <li key={attendee.userId} className="flex items-center gap-2">
                  <span className="min-w-0 flex-1 truncate text-body text-ink">
                    {attendee.name ?? `User ${attendee.userId}`}
                    {attendee.userId === currentUserId && (
                      <span className="text-ink-muted"> (you)</span>
                    )}
                  </span>
                  <span className="shrink-0 text-label-sm text-ink-muted">
                    {RESPONSE_LABEL[attendee.response]}
                  </span>
                  {canManage && (
                    <IconButton
                      size="tiny"
                      onClick={() => handleRemove(attendee)}
                      disabled={removingUserId === attendee.userId}
                      aria-label={`Remove ${attendee.name ?? "attendee"}`}
                    >
                      <Trash2 className="size-3.5" />
                    </IconButton>
                  )}
                </li>
              ))}
            </ul>
          ) : (
            <p className="mt-2 text-label-sm text-ink-muted">Nobody has been invited yet.</p>
          )}

          {canManage &&
            (targetOptions.length > 0 ? (
              <div className="mt-3 flex items-center gap-2">
                <Select
                  value={effectiveTarget}
                  onValueChange={setSelectedTarget}
                  options={targetOptions}
                  aria-label="Person or group to invite"
                  className="min-w-0 flex-1"
                />
                <Button
                  size="small"
                  onClick={handleInvite}
                  disabled={!effectiveTarget || isInviting}
                  loading={isInviting}
                >
                  Invite
                </Button>
              </div>
            ) : (
              <p className="mt-3 text-label-sm text-ink-muted">
                Everyone and every group in this workspace is already invited.
              </p>
            ))}

          {inviteError && (
            <p className="mt-2 text-label-sm text-danger" role="alert">
              {inviteError}
            </p>
          )}

          {ownAttendee && (
            <div className="mt-3">
              <p className="text-label-sm text-ink-muted">
                Your response: {RESPONSE_LABEL[ownAttendee.response]}
              </p>
              <div className="mt-1.5 flex items-center gap-2">
                <Button
                  size="small"
                  color="success"
                  onClick={() => handleRespond("accepted")}
                  disabled={isRespondingTo !== null || ownAttendee.response === "accepted"}
                  loading={isRespondingTo === "accepted"}
                >
                  Accept
                </Button>
                <Button
                  size="small"
                  variant="outline"
                  color="danger"
                  onClick={() => handleRespond("declined")}
                  disabled={isRespondingTo !== null || ownAttendee.response === "declined"}
                  loading={isRespondingTo === "declined"}
                >
                  Decline
                </Button>
                <Button
                  size="small"
                  variant="ghost"
                  color="secondary"
                  onClick={() => handleRespond("tentative")}
                  disabled={isRespondingTo !== null || ownAttendee.response === "tentative"}
                  loading={isRespondingTo === "tentative"}
                >
                  Tentative
                </Button>
              </div>
            </div>
          )}
        </>
      )}
    </div>
  );
}
