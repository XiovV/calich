import { ChevronLeft, ChevronRight, Settings } from "lucide-react";
import { useShellStore } from "../../lib/shellStore";
import { navigateDate } from "../../lib/navigateDate";
import { formatDateLabel } from "../../lib/formatDateLabel";
import { UserMenu } from "../../auth/UserMenu";
import { ViewSwitcher } from "./ViewSwitcher";

export function TopBar() {
  const selectedDate = useShellStore((state) => state.selectedDate);
  const activeView = useShellStore((state) => state.activeView);
  const setSelectedDate = useShellStore((state) => state.setSelectedDate);

  const goToToday = () => setSelectedDate(new Date());
  const goToPrevious = () =>
    setSelectedDate(navigateDate(selectedDate, activeView, "prev"));
  const goToNext = () =>
    setSelectedDate(navigateDate(selectedDate, activeView, "next"));

  return (
    <div className="flex h-full items-center gap-4 px-4">
      <span className="text-heading font-medium text-ink">Calendar</span>

      <button
        type="button"
        onClick={goToToday}
        className="rounded-shell-sm border border-border px-3 py-1.5 text-body text-ink hover:bg-surface-hover"
      >
        Today
      </button>

      <div className="flex items-center gap-1">
        <button
          type="button"
          onClick={goToPrevious}
          aria-label="Previous period"
          className="rounded-shell-pill p-1.5 text-ink-muted hover:bg-surface-hover"
        >
          <ChevronLeft className="size-5" />
        </button>
        <button
          type="button"
          onClick={goToNext}
          aria-label="Next period"
          className="rounded-shell-pill p-1.5 text-ink-muted hover:bg-surface-hover"
        >
          <ChevronRight className="size-5" />
        </button>
      </div>

      <span className="text-heading text-ink">
        {formatDateLabel(selectedDate, activeView)}
      </span>

      <div className="ml-auto flex items-center gap-2">
        <ViewSwitcher />
        <button
          type="button"
          aria-label="Settings"
          className="rounded-shell-pill p-1.5 text-ink-muted hover:bg-surface-hover"
        >
          <Settings className="size-5" />
        </button>
        <UserMenu />
      </div>
    </div>
  );
}
