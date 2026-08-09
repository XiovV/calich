import { useEffect, useMemo, useState } from "react";
import { Trash2 } from "lucide-react";
import {
  calendarsApi,
  type GroupShare,
  type Role,
  type Share,
  type ShareTargetGroup,
  type ShareTargetUser,
} from "../../lib/calendarsApi";
import { useAuthStore } from "../../lib/authStore";
import { useCalendarsStore } from "../../lib/calendarsStore";
import { useWorkspacesStore } from "../../lib/workspacesStore";
import { errorMessage } from "../../lib/errorMessage";
import { toast } from "../../lib/toast";
import type { Calendar } from "../../lib/calendar";
import { Button } from "../ui/Button";
import { IconButton } from "../ui/IconButton";
import { Select } from "../ui/Select";
import { fieldLabelClass } from "../ui/fieldStyles";

const ROLE_OPTIONS: { value: Role; label: string }[] = [
  { value: "viewer", label: "Viewer" },
  { value: "editor", label: "Editor" },
];

// TargetKey encodes a share-dialog picker entry as "user:<id>" or
// "group:<id>" so one Select can offer both Users and Groups (#159,
// ADR-0045) without a second control competing for the same row.
type TargetKey = `user:${number}` | `group:${number}`;

function userTargetKey(userId: number): TargetKey {
  return `user:${userId}`;
}
function groupTargetKey(groupId: number): TargetKey {
  return `group:${groupId}`;
}

interface CalendarSharingSectionProps {
  calendar: Calendar;
}

// CalendarSharingSection is the Owner-only sharing management surface
// (#113, ADR-0034; #159, ADR-0045) inside CalendarModal: who — User or
// Group — has Access to this Calendar and with what Role, granting a new
// Share, changing an existing one's Role in place, and revoking one.
// Renders only when a caller with canManageCalendar mounts it — see
// CalendarModal.
export function CalendarSharingSection({ calendar }: CalendarSharingSectionProps) {
  const accessToken = useAuthStore((state) => state.accessToken);
  const shareCalendar = useCalendarsStore((state) => state.shareCalendar);
  const revokeCalendarShare = useCalendarsStore((state) => state.revokeCalendarShare);
  const shareCalendarWithGroup = useCalendarsStore((state) => state.shareCalendarWithGroup);
  const revokeCalendarGroupShare = useCalendarsStore((state) => state.revokeCalendarGroupShare);
  const activeWorkspaceId = useWorkspacesStore((state) => state.activeWorkspaceId);
  const workspaces = useWorkspacesStore((state) => state.workspaces);

  // New Shares default to the Workspace's configured default-share-privacy
  // setting (#159): "private" defaults the picker to Viewer, "workspace" to
  // Editor.
  const defaultRole: Role =
    workspaces.find((w) => w.id === activeWorkspaceId)?.defaultSharePrivacy === "workspace"
      ? "editor"
      : "viewer";

  const [shares, setShares] = useState<Share[] | null>(null);
  const [groupShares, setGroupShares] = useState<GroupShare[] | null>(null);
  const [availableUsers, setAvailableUsers] = useState<ShareTargetUser[]>([]);
  const [availableGroups, setAvailableGroups] = useState<ShareTargetGroup[]>([]);
  const [selectedTarget, setSelectedTarget] = useState<TargetKey | "">("");
  // Seeded once from the Workspace's default-share-privacy setting (#159) —
  // a lazy initializer rather than an effect, since this only needs to run
  // once at mount and must never fight a caller's own later selection.
  const [selectedRole, setSelectedRole] = useState<Role>(() => defaultRole);
  const [isGranting, setIsGranting] = useState(false);
  const [grantError, setGrantError] = useState<string | null>(null);
  const [revokingKey, setRevokingKey] = useState<TargetKey | null>(null);

  useEffect(() => {
    if (!accessToken) return;
    let cancelled = false;
    (async () => {
      try {
        const [fetchedShares, fetchedGroupShares, targets] = await Promise.all([
          calendarsApi.listShares(accessToken, calendar.id),
          calendarsApi.listGroupShares(accessToken, calendar.id),
          calendarsApi.shareTargets(accessToken, calendar.id),
        ]);
        if (cancelled) return;
        setShares(fetchedShares);
        setGroupShares(fetchedGroupShares);
        setAvailableUsers(targets.users);
        setAvailableGroups(targets.groups);
      } catch {
        if (!cancelled) toast.error("Failed to load sharing settings.");
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [accessToken, calendar.id]);

  const sharedUserIds = new Set((shares ?? []).map((share) => share.userId));
  const sharedGroupIds = new Set((groupShares ?? []).map((share) => share.groupId));
  const pickableUsers = availableUsers.filter((user) => !sharedUserIds.has(user.userId));
  const pickableGroups = availableGroups.filter((group) => !sharedGroupIds.has(group.groupId));

  const targetOptions = useMemo(
    () => [
      ...pickableUsers.map((user) => ({
        value: userTargetKey(user.userId),
        label: user.username,
      })),
      ...pickableGroups.map((group) => ({
        value: groupTargetKey(group.groupId),
        label: `${group.name} (group)`,
      })),
    ],
    [pickableUsers, pickableGroups],
  );

  const effectiveTarget = targetOptions.some((option) => option.value === selectedTarget)
    ? selectedTarget
    : (targetOptions[0]?.value ?? "");

  async function handleGrant() {
    if (!accessToken || !effectiveTarget) return;
    const [kind, idPart] = effectiveTarget.split(":");
    setIsGranting(true);
    setGrantError(null);
    try {
      if (kind === "user") {
        const user = pickableUsers.find((u) => u.userId === Number(idPart));
        if (!user) return;
        const share = await shareCalendar(calendar.id, user.username, selectedRole);
        setShares((prev) => [...(prev ?? []), share]);
      } else {
        const groupId = Number(idPart);
        const share = await shareCalendarWithGroup(calendar.id, groupId, selectedRole);
        setGroupShares((prev) => [...(prev ?? []), share]);
      }
      setSelectedTarget("");
      setSelectedRole(defaultRole);
    } catch (err) {
      setGrantError(errorMessage(err));
    } finally {
      setIsGranting(false);
    }
  }

  async function handleUserRoleChange(share: Share, role: Role) {
    if (!accessToken) return;
    const previous = shares;
    setShares((prev) =>
      (prev ?? []).map((s) => (s.userId === share.userId ? { ...s, role } : s)),
    );
    try {
      await calendarsApi.share(accessToken, calendar.id, share.username, role);
    } catch (err) {
      setShares(previous);
      toast.error(errorMessage(err));
    }
  }

  async function handleGroupRoleChange(share: GroupShare, role: Role) {
    if (!accessToken) return;
    const previous = groupShares;
    setGroupShares((prev) =>
      (prev ?? []).map((s) => (s.groupId === share.groupId ? { ...s, role } : s)),
    );
    try {
      await calendarsApi.shareWithGroup(accessToken, calendar.id, share.groupId, role);
    } catch (err) {
      setGroupShares(previous);
      toast.error(errorMessage(err));
    }
  }

  async function handleRevokeUser(share: Share) {
    if (!accessToken) return;
    setRevokingKey(userTargetKey(share.userId));
    const previous = shares;
    setShares((prev) => (prev ?? []).filter((s) => s.userId !== share.userId));
    try {
      await revokeCalendarShare(calendar.id, share.userId);
    } catch {
      setShares(previous);
      toast.error(`Failed to remove ${share.username}'s access.`);
    } finally {
      setRevokingKey(null);
    }
  }

  async function handleRevokeGroup(share: GroupShare) {
    if (!accessToken) return;
    setRevokingKey(groupTargetKey(share.groupId));
    const previous = groupShares;
    setGroupShares((prev) => (prev ?? []).filter((s) => s.groupId !== share.groupId));
    try {
      await revokeCalendarGroupShare(calendar.id, share.groupId);
    } catch {
      setGroupShares(previous);
      toast.error(`Failed to remove ${share.groupName}'s access.`);
    } finally {
      setRevokingKey(null);
    }
  }

  const loading = shares === null || groupShares === null;

  return (
    <div className="mt-5 border-t border-border pt-4">
      <p className={fieldLabelClass}>Sharing</p>

      {loading ? (
        <p className="mt-2 text-label-sm text-ink-muted">Loading…</p>
      ) : (
        <>
          {shares.length > 0 || groupShares.length > 0 ? (
            <ul className="mt-2 flex max-h-40 flex-col gap-1.5 overflow-y-auto">
              {shares.map((share) => (
                <li key={`user-${share.userId}`} className="flex items-center gap-2">
                  <span className="min-w-0 flex-1 truncate text-body text-ink">
                    {share.username}
                  </span>
                  <Select
                    value={share.role}
                    onValueChange={(role) => handleUserRoleChange(share, role)}
                    options={ROLE_OPTIONS}
                    aria-label={`${share.username}'s role`}
                    className="shrink-0"
                  />
                  <IconButton
                    size="tiny"
                    onClick={() => handleRevokeUser(share)}
                    disabled={revokingKey === userTargetKey(share.userId)}
                    aria-label={`Remove ${share.username}'s access`}
                  >
                    <Trash2 className="size-3.5" />
                  </IconButton>
                </li>
              ))}
              {groupShares.map((share) => (
                <li key={`group-${share.groupId}`} className="flex items-center gap-2">
                  <span className="min-w-0 flex-1 truncate text-body text-ink">
                    {share.groupName} <span className="text-ink-muted">(group)</span>
                  </span>
                  <Select
                    value={share.role}
                    onValueChange={(role) => handleGroupRoleChange(share, role)}
                    options={ROLE_OPTIONS}
                    aria-label={`${share.groupName}'s role`}
                    className="shrink-0"
                  />
                  <IconButton
                    size="tiny"
                    onClick={() => handleRevokeGroup(share)}
                    disabled={revokingKey === groupTargetKey(share.groupId)}
                    aria-label={`Remove ${share.groupName}'s access`}
                  >
                    <Trash2 className="size-3.5" />
                  </IconButton>
                </li>
              ))}
            </ul>
          ) : (
            <p className="mt-2 text-label-sm text-ink-muted">
              Nobody else has Access to this calendar yet.
            </p>
          )}

          {targetOptions.length > 0 ? (
            <div className="mt-3 flex items-center gap-2">
              <Select
                value={effectiveTarget}
                onValueChange={setSelectedTarget}
                options={targetOptions}
                aria-label="Person or group to share with"
                className="min-w-0 flex-1"
              />
              <Select
                value={selectedRole}
                onValueChange={setSelectedRole}
                options={ROLE_OPTIONS}
                aria-label="Role"
                className="shrink-0"
              />
              <Button
                size="small"
                onClick={handleGrant}
                disabled={!effectiveTarget || isGranting}
                loading={isGranting}
              >
                Add
              </Button>
            </div>
          ) : (
            <p className="mt-3 text-label-sm text-ink-muted">
              {availableUsers.length === 0 && availableGroups.length === 0
                ? "Nobody else in this workspace to share with yet."
                : "Everyone and every group in this workspace already has Access."}
            </p>
          )}

          {grantError && (
            <p className="mt-2 text-label-sm text-danger" role="alert">
              {grantError}
            </p>
          )}
        </>
      )}
    </div>
  );
}
