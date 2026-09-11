import { useEffect } from "react";
import { Menu } from "@base-ui/react/menu";
import { Check, ChevronDown } from "lucide-react";
import { useNavigate } from "react-router";
import { useShellStore } from "../../lib/shellStore";
import { useCalendarSetsStore } from "../../lib/calendarSetsStore";
import { useWorkspacesStore } from "../../lib/workspacesStore";

// The Menu's radio value space is strings; ALL_CALENDARS stands in for "no
// Active Calendar Set" — the absence of a Set, not a row of its own
// (ADR-0082) — since a Set's own id (a number) can never collide with it.
const ALL_CALENDARS = "all";

// Matches CalendarList.tsx's and UserMenu.tsx's own Menu.Item classes
// (cursor-default, edge-to-edge highlight), not Select.tsx's — this menu is
// built on the Menu primitive, not Select, and its items should look like
// this codebase's other menus rather than a dropdown's.
const itemClasses =
  "flex cursor-default items-center gap-3 px-3 py-1.5 text-body text-ink data-[highlighted]:bg-surface-hover";

// The top-bar Calendar Set switcher (#303, #304, ADR-0082): lists "All
// calendars" plus the caller's own Sets in the active Workspace, and selects
// between them. Built on the Menu primitive rather than WorkspaceSwitcher's
// Select (ADR-0082) so it can carry a divider and a "Manage sets" entry
// alongside the Sets themselves. Always rendered once a Workspace is active,
// even at zero Sets, so the feature is discoverable before the caller has
// made one, and creating the first Set never changes the trigger's own
// label — only selecting it does — so the top bar never reflows from that
// alone.
export function CalendarSetSwitcher() {
  const navigate = useNavigate();
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

  const activeCalendarSet = calendarSets.find((calendarSet) => calendarSet.id === activeCalendarSetId);
  const radioValue = activeCalendarSet ? String(activeCalendarSet.id) : ALL_CALENDARS;

  return (
    <Menu.Root>
      <Menu.Trigger
        aria-label="Select calendar set"
        className="flex cursor-pointer items-center gap-1.5 rounded-shell-pill bg-surface-sunken px-4 py-1.5 text-body text-ink ring-1 ring-border transition-colors outline-none hover:bg-surface-hover data-[popup-open]:ring-2 data-[popup-open]:ring-accent-ink"
      >
        <span className="min-w-0 truncate">{activeCalendarSet?.name ?? "All calendars"}</span>
        <ChevronDown className="size-4 shrink-0 text-ink-muted" />
      </Menu.Trigger>
      <Menu.Portal>
        <Menu.Positioner sideOffset={4} className="z-[60]">
          <Menu.Popup className="min-w-[--anchor-width] rounded-shell-md border border-border bg-surface py-1.5 shadow-elevation-2">
            <Menu.RadioGroup
              value={radioValue}
              onValueChange={(value: string) =>
                setActiveCalendarSetId(value === ALL_CALENDARS ? null : Number(value))
              }
            >
              <Menu.RadioItem value={ALL_CALENDARS} closeOnClick className={itemClasses}>
                <Menu.RadioItemIndicator keepMounted className="flex size-4 shrink-0 items-center justify-center data-[unchecked]:opacity-0">
                  <Check className="size-4 text-accent-ink" />
                </Menu.RadioItemIndicator>
                All calendars
              </Menu.RadioItem>
              {calendarSets.map((calendarSet) => (
                <Menu.RadioItem key={calendarSet.id} value={String(calendarSet.id)} closeOnClick className={itemClasses}>
                  <Menu.RadioItemIndicator keepMounted className="flex size-4 shrink-0 items-center justify-center data-[unchecked]:opacity-0">
                    <Check className="size-4 text-accent-ink" />
                  </Menu.RadioItemIndicator>
                  <span className="min-w-0 truncate">{calendarSet.name}</span>
                </Menu.RadioItem>
              ))}
            </Menu.RadioGroup>
            <div role="separator" className="my-1 border-t border-border" />
            <Menu.Item onClick={() => navigate("/settings/calendar-sets")} className={itemClasses}>
              Manage sets
            </Menu.Item>
          </Menu.Popup>
        </Menu.Positioner>
      </Menu.Portal>
    </Menu.Root>
  );
}
