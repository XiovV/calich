import { useEffect, useState } from "react";
import { Trash2 } from "lucide-react";
import { calendarsApi, type Role, type Share } from "../../lib/calendarsApi";
import { usersApi, type UserSummary } from "../../lib/usersApi";
import { useAuthStore } from "../../lib/authStore";
import { useCalendarsStore } from "../../lib/calendarsStore";
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

interface CalendarSharingSectionProps {
  calendar: Calendar;
}

// CalendarSharingSection is the Owner-only sharing management surface
// (#113, ADR-0034) inside CalendarModal: who has Access to this Calendar and
// with what Role, granting a new Share, changing an existing one's Role in
// place, and revoking one. Renders only when a caller with canManageCalendar
// mounts it — see CalendarModal.
export function CalendarSharingSection({ calendar }: CalendarSharingSectionProps) {
  const accessToken = useAuthStore((state) => state.accessToken);
  const shareCalendar = useCalendarsStore((state) => state.shareCalendar);
  const revokeCalendarShare = useCalendarsStore((state) => state.revokeCalendarShare);

  const [shares, setShares] = useState<Share[] | null>(null);
  const [directory, setDirectory] = useState<UserSummary[]>([]);
  const [selectedUsername, setSelectedUsername] = useState("");
  const [selectedRole, setSelectedRole] = useState<Role>("viewer");
  const [isGranting, setIsGranting] = useState(false);
  const [grantError, setGrantError] = useState<string | null>(null);
  const [revokingUserId, setRevokingUserId] = useState<number | null>(null);

  useEffect(() => {
    if (!accessToken) return;
    let cancelled = false;
    (async () => {
      try {
        const [fetchedShares, fetchedDirectory] = await Promise.all([
          calendarsApi.listShares(accessToken, calendar.id),
          usersApi.directory(accessToken),
        ]);
        if (cancelled) return;
        setShares(fetchedShares);
        setDirectory(fetchedDirectory);
      } catch {
        if (!cancelled) toast.error("Failed to load sharing settings.");
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [accessToken, calendar.id]);

  const sharedUserIds = new Set((shares ?? []).map((share) => share.userId));
  const availableUsers = directory.filter((user) => !sharedUserIds.has(user.id));
  const effectiveUsername = availableUsers.some((user) => user.username === selectedUsername)
    ? selectedUsername
    : (availableUsers[0]?.username ?? "");

  async function handleGrant() {
    if (!accessToken || !effectiveUsername) return;
    setIsGranting(true);
    setGrantError(null);
    try {
      const share = await shareCalendar(calendar.id, effectiveUsername, selectedRole);
      setShares((prev) => [...(prev ?? []), share]);
      setSelectedUsername("");
      setSelectedRole("viewer");
    } catch (err) {
      setGrantError(errorMessage(err));
    } finally {
      setIsGranting(false);
    }
  }

  async function handleRoleChange(share: Share, role: Role) {
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

  async function handleRevoke(share: Share) {
    if (!accessToken) return;
    setRevokingUserId(share.userId);
    const previous = shares;
    setShares((prev) => (prev ?? []).filter((s) => s.userId !== share.userId));
    try {
      await revokeCalendarShare(calendar.id, share.userId);
    } catch {
      setShares(previous);
      toast.error(`Failed to remove ${share.username}'s access.`);
    } finally {
      setRevokingUserId(null);
    }
  }

  return (
    <div className="mt-5 border-t border-border pt-4">
      <p className={fieldLabelClass}>Sharing</p>

      {shares === null ? (
        <p className="mt-2 text-label-sm text-ink-muted">Loading…</p>
      ) : (
        <>
          {shares.length > 0 ? (
            <ul className="mt-2 flex max-h-40 flex-col gap-1.5 overflow-y-auto">
              {shares.map((share) => (
                <li key={share.userId} className="flex items-center gap-2">
                  <span className="min-w-0 flex-1 truncate text-body text-ink">
                    {share.username}
                  </span>
                  <Select
                    value={share.role}
                    onValueChange={(role) => handleRoleChange(share, role)}
                    options={ROLE_OPTIONS}
                    aria-label={`${share.username}'s role`}
                    className="shrink-0"
                  />
                  <IconButton
                    size="tiny"
                    onClick={() => handleRevoke(share)}
                    disabled={revokingUserId === share.userId}
                    aria-label={`Remove ${share.username}'s access`}
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

          {availableUsers.length > 0 ? (
            <div className="mt-3 flex items-center gap-2">
              <Select
                value={effectiveUsername}
                onValueChange={setSelectedUsername}
                options={availableUsers.map((user) => ({
                  value: user.username,
                  label: user.username,
                }))}
                aria-label="Person to share with"
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
                disabled={!effectiveUsername || isGranting}
                loading={isGranting}
              >
                Add
              </Button>
            </div>
          ) : (
            <p className="mt-3 text-label-sm text-ink-muted">
              {directory.length === 0
                ? "No other users on this instance yet."
                : "Everyone on this instance already has Access."}
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
