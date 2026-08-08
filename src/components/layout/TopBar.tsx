import { ChevronLeft, ChevronRight, Settings } from "lucide-react";
import { useNavigate } from "react-router";
import { useShellStore } from "../../lib/shellStore";
import { useWeekStartsOn } from "../../hooks/useWeekStartsOn";
import { navigateDate } from "../../lib/navigateDate";
import { formatDateLabel } from "../../lib/formatDateLabel";
import { UserMenu } from "../../auth/UserMenu";
import { ViewSwitcher } from "./ViewSwitcher";
import { ThemeToggle } from "./ThemeToggle";
import { NotificationBell } from "./NotificationBell";
import { Button } from "../ui/Button";
import { IconButton } from "../ui/IconButton";

export function TopBar() {
  const navigate = useNavigate();
  const selectedDate = useShellStore((state) => state.selectedDate);
  const activeView = useShellStore((state) => state.activeView);
  const setSelectedDate = useShellStore((state) => state.setSelectedDate);
  const weekStartsOn = useWeekStartsOn();

  const goToToday = () => setSelectedDate(new Date());
  const goToPrevious = () =>
    setSelectedDate(navigateDate(selectedDate, activeView, "prev"));
  const goToNext = () =>
    setSelectedDate(navigateDate(selectedDate, activeView, "next"));

  return (
    <div className="flex h-full items-center gap-4 px-4">
      <span className="text-heading font-medium text-ink">Calendar</span>

      <Button
        variant="outline"
        color="secondary"
        size="small"
        onClick={goToToday}
      >
        Today
      </Button>

      <div className="flex items-center gap-1">
        <IconButton onClick={goToPrevious} aria-label="Previous period">
          <ChevronLeft className="size-5" />
        </IconButton>
        <IconButton onClick={goToNext} aria-label="Next period">
          <ChevronRight className="size-5" />
        </IconButton>
      </div>

      <span className="text-heading text-ink">
        {formatDateLabel(selectedDate, activeView, weekStartsOn)}
      </span>

      <div className="ml-auto flex items-center gap-2">
        <ViewSwitcher />
        <ThemeToggle />
        <NotificationBell />
        <IconButton
          onClick={() => navigate("/settings")}
          aria-label="Settings"
        >
          <Settings className="size-5" />
        </IconButton>
        <UserMenu />
      </div>
    </div>
  );
}
