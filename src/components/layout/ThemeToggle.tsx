import { Menu } from "@base-ui/react/menu";
import { Check, Monitor, Moon, Sun } from "lucide-react";
import {
  useThemeStore,
  type ThemePreference,
} from "../../lib/themeStore";
import { iconButtonClasses } from "../ui/iconButtonClasses";

const OPTIONS: { value: ThemePreference; label: string; Icon: typeof Sun }[] = [
  { value: "light", label: "Light", Icon: Sun },
  { value: "dark", label: "Dark", Icon: Moon },
  { value: "system", label: "System", Icon: Monitor },
];

export function ThemeToggle() {
  const preference = useThemeStore((state) => state.preference);
  const setPreference = useThemeStore((state) => state.setPreference);

  const TriggerIcon =
    OPTIONS.find((option) => option.value === preference)?.Icon ?? Monitor;

  return (
    <Menu.Root>
      <Menu.Trigger aria-label="Theme" title="Theme" className={iconButtonClasses()}>
        <TriggerIcon className="size-5" />
      </Menu.Trigger>
      <Menu.Portal>
        <Menu.Positioner sideOffset={4} align="end" className="z-[60]">
          <Menu.Popup className="rounded-shell-md border border-border bg-surface py-1 shadow-elevation-2">
            {OPTIONS.map(({ value, label, Icon }) => (
              <Menu.Item
                key={value}
                onClick={() => setPreference(value)}
                className="flex cursor-default items-center gap-2 px-3 py-1.5 text-body text-ink data-[highlighted]:bg-surface-hover"
              >
                <Icon className="size-4" />
                <span className="flex-1">{label}</span>
                <Check
                  className={`size-4 text-accent-ink ${
                    preference === value ? "opacity-100" : "opacity-0"
                  }`}
                />
              </Menu.Item>
            ))}
          </Menu.Popup>
        </Menu.Positioner>
      </Menu.Portal>
    </Menu.Root>
  );
}
