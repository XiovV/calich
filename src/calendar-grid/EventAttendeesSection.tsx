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

// StagedAttendeeTarget is a create-mode invite that hasn't been sent yet
// (#187, ADR-0055): the create POST carries the whole list at once, so
// there is nothing to commit until Save. A staged Group renders as a
// single chip and expands to its members server-side, at save — not here.
export type StagedAttendeeTarget =
  | { kind: "user"; userId: number; name: string }
  | { kind: "group"; groupId: number; name: string };

// StagedAttendees is EventAttendeesSection's create-mode contract: the
// parent (EventModal) owns the staged list, exactly like it owns
// attachmentDrafts, so Save can read it without a round trip through this
// component. error is the create POST's rejection, shown as a banner here
// rather than a toast, since it's specific to what was staged.
export interface StagedAttendees {
  targets: StagedAttendeeTarget[];
  onChange: (targets: StagedAttendeeTarget[]) => void;
  error: string | null;
}

interface EventAttendeesSectionProps {
  // The Master Event Attendees are invited to (ADR-0046) — undefined in
  // create mode, where there's no id yet to invite anyone to. An Override
  // never carries its own, mirroring Attachments' own Master-scoping.
  eventId: string | undefined;
  // Whether the caller may invite/remove Attendees — an Editor or Owner of
  // the Event's Calendar in edit mode, or simply "a Calendar is selected"
  // in create mode, since there's no Access to check yet. False renders the
  // list read-only.
  canManage: boolean;
  // The Organizer row (ADR-0055): the Event's creator, rendered
  // non-removable and above the Attendees, in both modes — the signed-in
  // caller in create mode, sourced from events.created_by once known in
  // edit mode. Undefined when there's nobody to show (a deleted creator,
  // or an Event predating the column).
  organizerName: string | undefined;
  // Present only in create mode (#187, ADR-0055); absent in edit mode,
  // where an invite still commits the instant it's issued.
  staging?: StagedAttendees;
}

// EventAttendeesSection is the Attendee surface inside EventModal (#168,
// #187, ADR-0046, ADR-0055): an Organizer row, who's invited and their
// Response, and an invite picker (User or Group, scoped to the Event's
// Calendar's Workspace) for whoever can manage the Event. In edit mode an
// invite commits immediately; in create mode it's staged locally via
// `staging` until Save. The signed-in caller's own Accept/Decline/
// Tentative — independent of canManage — only ever applies once there's a
// real eventId to respond to.
export function EventAttendeesSection({
  eventId,
  canManage,
  organizerName,
  staging,
}: EventAttendeesSectionProps) {
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

  const isStaging = staging !== undefined;

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
    if (!accessToken || !canManage || (!eventId && !staging)) return;
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
    // staging?.error is a deliberate dependency in place of staging itself:
    // a rejected explicit target (ADR-0055) means this list went stale, so
    // a fresh failure re-fetches it, mirroring "refreshes its member list"
    // from the ADR's consequences — depending on staging (a fresh object
    // every render) would instead refetch on every keystroke.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [accessToken, eventId, canManage, staging?.error]);

  const stagedUserIds = new Set(
    isStaging
      ? staging.targets.filter((t) => t.kind === "user").map((t) => t.userId)
      : [],
  );
  const stagedGroupIds = new Set(
    isStaging
      ? staging.targets.filter((t) => t.kind === "group").map((t) => t.groupId)
      : [],
  );

  const invitedUserIds = new Set((attendees ?? []).map((a) => a.userId));
  const pickableUsers = availableUsers.filter(
    (user) => !invitedUserIds.has(user.userId) && !stagedUserIds.has(user.userId),
  );
  const pickableGroups = availableGroups.filter((group) => !stagedGroupIds.has(group.id));

  const targetOptions = useMemo(
    () => [
      ...pickableUsers.map((user) => ({
        value: userTargetKey(user.userId),
        label: user.name,
      })),
      ...pickableGroups.map((group) => ({
        value: groupTargetKey(group.id),
        label: `${group.name} (group)`,
      })),
    ],
    [pickableUsers, pickableGroups],
  );

  const effectiveTarget = targetOptions.some((option) => option.value === selectedTarget)
    ? selectedTarget
    : (targetOptions[0]?.value ?? "");

  async function handleInvite() {
    if (!effectiveTarget) return;
    const [kind, idPart] = effectiveTarget.split(":");

    if (staging) {
      if (kind === "user") {
        const user = pickableUsers.find((u) => u.userId === Number(idPart));
        if (!user) return;
        staging.onChange([...staging.targets, { kind: "user", userId: user.userId, name: user.name }]);
      } else {
        const group = pickableGroups.find((g) => g.id === Number(idPart));
        if (!group) return;
        staging.onChange([...staging.targets, { kind: "group", groupId: group.id, name: group.name }]);
      }
      setSelectedTarget("");
      return;
    }

    if (!accessToken || !eventId) return;
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

  function handleRemoveStaged(target: StagedAttendeeTarget) {
    if (!staging) return;
    staging.onChange(staging.targets.filter((t) => t !== target));
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

  if (!eventId && !staging) return null;

  const loading = !isStaging && attendees === null;
  const ownAttendee = attendees?.find((a) => a.userId === currentUserId);
  const stagedCount = isStaging ? staging.targets.length : 0;
  const attendeeCount = isStaging ? stagedCount : (attendees?.length ?? 0);
  const hasAnyRows = Boolean(organizerName) || attendeeCount > 0;

  if (!isStaging && !loading && !canManage && attendeeCount === 0 && !organizerName) return null;

  const bannerError = isStaging ? staging.error : inviteError;

  return (
    <div className="mt-4 border-t border-border pt-4">
      <p className={fieldLabelClass}>Attendees</p>

      {loading ? (
        <p className="mt-1.5 text-label-sm text-ink-muted">Loading…</p>
      ) : (
        <>
          {hasAnyRows ? (
            <ul className="mt-2 flex max-h-40 flex-col gap-1.5 overflow-y-auto">
              {organizerName && (
                <li className="flex items-center gap-2">
                  <span className="min-w-0 flex-1 truncate text-body text-ink">
                    {organizerName}
                  </span>
                  <span className="shrink-0 text-label-sm text-ink-muted">Organizer</span>
                </li>
              )}
              {isStaging
                ? staging.targets.map((target) => {
                    const key = target.kind === "user" ? `user:${target.userId}` : `group:${target.groupId}`;
                    const label = target.kind === "group" ? `${target.name} (group)` : target.name;
                    return (
                      <li key={key} className="flex items-center gap-2">
                        <span className="min-w-0 flex-1 truncate text-body text-ink">{label}</span>
                        <IconButton
                          size="tiny"
                          onClick={() => handleRemoveStaged(target)}
                          aria-label={`Remove ${target.name}`}
                        >
                          <Trash2 className="size-3.5" />
                        </IconButton>
                      </li>
                    );
                  })
                : attendees?.map((attendee) => (
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

          {bannerError && (
            <p className="mt-2 text-label-sm text-danger" role="alert">
              {bannerError}
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
