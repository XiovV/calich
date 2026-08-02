import { Select } from "../ui/Select";
import { useShellStore, type ActiveView } from "../../lib/shellStore";

const VIEW_OPTIONS: { value: ActiveView; label: string }[] = [
  { value: "day", label: "Day" },
  { value: "week", label: "Week" },
  { value: "month", label: "Month" },
  { value: "year", label: "Year" },
];

export function ViewSwitcher() {
  const activeView = useShellStore((state) => state.activeView);
  const setActiveView = useShellStore((state) => state.setActiveView);

  return (
    <Select
      value={activeView}
      onValueChange={setActiveView}
      options={VIEW_OPTIONS}
      aria-label="Select calendar view"
    />
  );
}
