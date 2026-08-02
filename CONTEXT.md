# Calendar

Domain vocabulary for the calendar app — navigation chrome, view state, and the sidebar calendar list.

## Language

**Active view**:
The granularity at which the calendar is currently displayed — Day, Week, Month, or Year. Drives the main content grid (later) and the top bar's date label format.
_Avoid_: mode, display mode

**Selected date**:
The single date the shell is currently focused on, shown in the mini calendar and driving the main grid. Distinct from "today" — navigating away doesn't change what "today" means.
_Avoid_: current date, active date

**Calendar**:
A named, colored, independently-toggleable collection that groups events (e.g. "Work", "Personal"). Not the app itself, and not the view/grid.
_Avoid_: calendar list item, source

**Event**:
A titled time block belonging to a single Calendar, with a start, an end, and zero or more Reminders. Rendered on the grid in its Calendar's color.
_Avoid_: appointment, meeting, entry

**Draft block**:
The in-progress time selection made by dragging on the grid, before it is confirmed into an Event via the creation modal. Discarded, not saved, if the modal is cancelled.
_Avoid_: pending event, temp event

**Reminder**:
A minutes-before-start offset attached to an Event, chosen from a fixed preset list (not free-form).
_Avoid_: notification, alert
