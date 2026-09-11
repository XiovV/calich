import { useEffect, useState } from "react";
import { Calendar as CalendarIcon, Check, Pencil, Plus, Trash2, X } from "lucide-react";
import { Button } from "../components/ui/Button";
import { IconButton } from "../components/ui/IconButton";
import { Input } from "../components/ui/Input";
import { useAsyncAction } from "../hooks/useAsyncAction";
import { errorMessage } from "../lib/errorMessage";
import { useWorkspacesStore } from "../lib/workspacesStore";
import { useCalendarSetsStore } from "../lib/calendarSetsStore";
import type { CalendarSet } from "../lib/calendarSetsApi";
import { DeleteCalendarSetDialog } from "./DeleteCalendarSetDialog";
import { CalendarSetMembershipDialog } from "./CalendarSetMembershipDialog";

// The Calendar Sets Settings Section (#301, ADR-0082): create, rename and
// delete the caller's own Calendar Sets — a named, private selection of the
// active Workspace's Calendars. Sits in Settings' Personal group because a
// Set is private outright: no sharing mechanism, no Role, no Admin surface,
// unlike GroupsSection this needs no Owner/Admin gate, since every route is
// already scoped to the caller by (user, active workspace). Re-renders on
// Workspace switch, mirroring GroupsSection's own effect.
export function CalendarSetsSection() {
  const activeWorkspaceId = useWorkspacesStore((state) => state.activeWorkspaceId);

  const calendarSets = useCalendarSetsStore((state) => state.calendarSets);
  const fetchCalendarSets = useCalendarSetsStore((state) => state.fetchCalendarSets);
  const createCalendarSet = useCalendarSetsStore((state) => state.createCalendarSet);
  const renameCalendarSet = useCalendarSetsStore((state) => state.renameCalendarSet);

  const [loadError, setLoadError] = useState<string | null>(null);
  const [name, setName] = useState("");
  const [editingId, setEditingId] = useState<number | null>(null);
  const [editingName, setEditingName] = useState("");
  const [deleteTarget, setDeleteTarget] = useState<CalendarSet | null>(null);
  const [membersTarget, setMembersTarget] = useState<CalendarSet | null>(null);
  const { isSubmitting: isCreating, error: createError, run: runCreate } = useAsyncAction();
  const { isSubmitting: isRenaming, error: renameError, setError: setRenameError, run: runRename } = useAsyncAction();

  useEffect(() => {
    if (activeWorkspaceId === null) return;
    fetchCalendarSets()
      .then(() => setLoadError(null))
      .catch((err) => setLoadError(errorMessage(err)));
  }, [activeWorkspaceId, fetchCalendarSets]);

  async function handleCreate(domEvent: React.FormEvent) {
    domEvent.preventDefault();
    if (!name.trim()) return;

    await runCreate(async () => {
      await createCalendarSet(name.trim());
      setName("");
    });
  }

  function startEditing(calendarSet: CalendarSet) {
    setEditingId(calendarSet.id);
    setEditingName(calendarSet.name);
    setRenameError(null);
  }

  async function handleRename(calendarSet: CalendarSet) {
    const trimmed = editingName.trim();
    if (!trimmed || trimmed === calendarSet.name) {
      setEditingId(null);
      return;
    }

    await runRename(async () => {
      await renameCalendarSet(calendarSet.id, trimmed);
      setEditingId(null);
    });
  }

  return (
    <section>
      <h2 className="text-heading font-medium text-ink">Calendar sets</h2>
      <p className="mt-1 text-body text-ink-muted">
        Save named selections of this workspace's calendars, private to you, so you can switch your view in one
        click.
      </p>

      {loadError && <p className="mt-2 text-label-sm text-danger">{loadError}</p>}

      <form onSubmit={handleCreate} className="mt-4 flex items-end gap-2">
        <Input
          label="New set"
          placeholder="Work"
          value={name}
          onChange={(domEvent) => setName(domEvent.target.value)}
          className="w-72"
        />
        <Button type="submit" disabled={!name.trim()} loading={isCreating} leadingIcon={<Plus className="size-4" />}>
          Create
        </Button>
      </form>

      {createError && <p className="mt-2 text-label-sm text-danger">{createError}</p>}

      <ul className="mt-4 flex flex-col gap-2">
        {calendarSets.map((calendarSet) => {
          const isEditing = editingId === calendarSet.id;
          return (
            <li key={calendarSet.id} className="rounded-md border border-border px-3 py-2">
              <div className="flex items-center justify-between gap-2">
                {isEditing ? (
                  <Input
                    autoFocus
                    value={editingName}
                    onChange={(domEvent) => setEditingName(domEvent.target.value)}
                    onKeyDown={(domEvent) => {
                      if (domEvent.key === "Enter") handleRename(calendarSet);
                      if (domEvent.key === "Escape") setEditingId(null);
                    }}
                    className="flex-1"
                    aria-label={`Rename ${calendarSet.name}`}
                  />
                ) : (
                  <p className="text-body text-ink">{calendarSet.name}</p>
                )}

                <div className="flex items-center gap-1">
                  {isEditing ? (
                    <>
                      <IconButton
                        onClick={() => handleRename(calendarSet)}
                        disabled={isRenaming}
                        aria-label={`Save ${calendarSet.name}`}
                      >
                        <Check className="size-4" />
                      </IconButton>
                      <IconButton onClick={() => setEditingId(null)} disabled={isRenaming} aria-label="Cancel rename">
                        <X className="size-4" />
                      </IconButton>
                    </>
                  ) : (
                    <>
                      <IconButton
                        onClick={() => setMembersTarget(calendarSet)}
                        aria-label={`Manage ${calendarSet.name} calendars`}
                      >
                        <CalendarIcon className="size-4" />
                      </IconButton>
                      <IconButton onClick={() => startEditing(calendarSet)} aria-label={`Rename ${calendarSet.name}`}>
                        <Pencil className="size-4" />
                      </IconButton>
                      <IconButton onClick={() => setDeleteTarget(calendarSet)} aria-label={`Delete ${calendarSet.name}`}>
                        <Trash2 className="size-4" />
                      </IconButton>
                    </>
                  )}
                </div>
              </div>
              {isEditing && renameError && <p className="mt-2 text-label-sm text-danger">{renameError}</p>}
            </li>
          );
        })}

        {calendarSets.length === 0 && !loadError && (
          <p className="text-label-sm text-ink-muted">No calendar sets yet.</p>
        )}
      </ul>

      {deleteTarget && <DeleteCalendarSetDialog calendarSet={deleteTarget} onClose={() => setDeleteTarget(null)} />}
      {membersTarget && (
        <CalendarSetMembershipDialog calendarSet={membersTarget} onClose={() => setMembersTarget(null)} />
      )}
    </section>
  );
}
