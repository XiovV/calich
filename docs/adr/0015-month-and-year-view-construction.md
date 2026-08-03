# Month and Year views: hand-rolled month grid, react-day-picker year grid

The Month and Year views are built with deliberately different foundations rather than being generalized alongside the Day/Week `TimeGrid` (ADR-0007) or forced through one shared component.

**Month view is hand-rolled** (`calendar-grid/MonthGrid.tsx`): a fixed six-row calendar grid with Event chips, dynamic per-cell overflow ("+N more" → Day view), custom click targets (chip → edit, day number → Day view, empty cell → create), and drag-to-move. It reuses `TimeGrid`'s manual `mousemove`/`mouseup` drag engine (not HTML5 DnD, so no touch-drag — matching current grid behavior) because a month cell has no time axis, so the rich `TimeGrid` layout doesn't apply. `react-day-picker` was rejected here: it can't express chips, overflow, or drag-to-move.

**Year view reuses `react-day-picker`** (`calendar-grid/YearGrid.tsx`): twelve read-only Mini-months in a 4×3 grid, using the library's `modifiers` for Event-presence dots and `onDayClick`/caption for navigation. It's already a dependency (the sidebar `MiniCalendar`) and Year view is exactly what it's good at, so hand-rolling it would rebuild that for free — the inconsistency with the hand-rolled Month grid is worth the saved effort.

**Events stay single-day.** Month view supports drag-to-**move** (drop changes the date; time-of-day and duration are preserved) but deliberately **not** drag-to-create: creating by sweeping across cells would imply multi-day/all-day Events, which the model doesn't have (`EventModal` pins start and end to one day). A future reader wondering "why can't I drag out a new event in Month view like in Week view?" — this is why. Adding multi-day/all-day Events is a separate, larger decision, not part of this work.
