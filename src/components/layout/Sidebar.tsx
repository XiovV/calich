import { Plus } from "lucide-react";
import { MiniCalendar } from "./MiniCalendar";
import { CalendarList } from "./CalendarList";
import { Button } from "../ui/Button";

interface SidebarProps {
  onCreateClick: () => void;
}

export function Sidebar({ onCreateClick }: SidebarProps) {
  return (
    <div className="flex h-full flex-col gap-3 overflow-x-hidden overflow-y-auto py-6 pe-3">
      <Button
        leadingIcon={<Plus className="size-4" />}
        onClick={onCreateClick}
        className="ms-4 self-start"
      >
        Create
      </Button>
      <MiniCalendar />
      <CalendarList />
    </div>
  );
}
