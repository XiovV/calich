import { Plus } from "lucide-react";
import { MiniCalendar } from "./MiniCalendar";
import { CalendarList } from "./CalendarList";

export function Sidebar() {
  return (
    <div className="flex flex-col gap-4 overflow-y-auto py-4">
      <button
        type="button"
        className="mx-4 flex items-center gap-2 self-start rounded-shell-md border border-border bg-surface px-4 py-2 text-body text-ink shadow-elevation-1 hover:bg-surface-hover"
      >
        <Plus className="size-4" />
        Create
      </button>
      <MiniCalendar />
      <CalendarList />
    </div>
  );
}
