import { useEffect } from "react";
import { Select } from "../ui/Select";
import { useShellStore } from "../../lib/shellStore";
import { useCalendarSetsStore } from "../../lib/calendarSetsStore";
import { useWorkspacesStore } from "../../lib/workspacesStore";

// The Select's value space is strings; ALL_CALENDARS stands in for "no
// Active Calendar Set" — the absence of a Set, not a row of its own
// (ADR-0082) — since a Set's own id (a number) can never collide with it.
const ALL_CALENDARS = "all";

// The top-bar Calendar Set switcher (#303, ADR-0082): lists "All calendars"
// plus the caller's own Sets in the active Workspace, and selects between
// them. Built on Select for now, matching WorkspaceSwitcher — #304 upgrades
// it to the Menu primitive once it needs to carry a divider and a "Manage
// sets" entry, and adds the zero-Set/reset/deleted-while-active handling
// this ticket deliberately leaves out.
export function CalendarSetSwitcher() {
  const activeWorkspaceId = useWorkspacesStore((state) => state.activeWorkspaceId);
  const calendarSets = useCalendarSetsStore((state) => state.calendarSets);
  const fetchCalendarSets = useCalendarSetsStore((state) => state.fetchCalendarSets);
  const activeCalendarSetId = useShellStore((state) => state.activeCalendarSetId);
  const setActiveCalendarSetId = useShellStore((state) => state.setActiveCalendarSetId);

  useEffect(() => {
    if (activeWorkspaceId === null) return;
    // A failure here costs the switcher its Set options and nothing else —
    // "All calendars" still renders and still selects, same as CalendarList's
    // own fetchConnections effect.
    fetchCalendarSets().catch(() => {});
  }, [activeWorkspaceId, fetchCalendarSets]);

  if (activeWorkspaceId === null) return null;

  return (
    <Select
      value={activeCalendarSetId === null ? ALL_CALENDARS : String(activeCalendarSetId)}
      onValueChange={(value) =>
        setActiveCalendarSetId(value === ALL_CALENDARS ? null : Number(value))
      }
      options={[
        { value: ALL_CALENDARS, label: "All calendars" },
        ...calendarSets.map((calendarSet) => ({
          value: String(calendarSet.id),
          label: calendarSet.name,
        })),
      ]}
      aria-label="Select calendar set"
    />
  );
}
