import { Dialog } from "@base-ui/react/dialog";
import { X } from "lucide-react";
import { NavLink, Outlet, useNavigate } from "react-router";
import { iconButtonClasses } from "../components/ui/iconButtonClasses";
import {
  getSettingsSections,
  SETTINGS_GROUP_LABELS,
  SETTINGS_GROUP_ORDER,
} from "./settingsSections";

// Settings as a modal over the calendar (#176, ADR-0049): rendered from a
// child route of AppShell, so it paints on top of a calendar that's still
// mounted underneath rather than replacing it. Sits one rung below the
// eight dialogs Settings itself owns (z-40/z-50, untouched) so a confirmation
// like "Delete your account?" opens above a dimmed Settings instead of under
// a fully-lit one. Closing (Escape, outside click, or the ✕) replaces the
// history entry with "/", so a tab opened directly on a Section closes to
// the calendar rather than out of the app; switching Section also replaces,
// so Back doesn't walk through every previously-visited Section.
export function SettingsModal() {
  const navigate = useNavigate();
  const sections = getSettingsSections();

  return (
    <Dialog.Root
      open
      onOpenChange={(open) => {
        if (!open) navigate("/", { replace: true });
      }}
    >
      <Dialog.Portal>
        <Dialog.Backdrop className="fixed inset-0 z-30 bg-ink/20" />
        <Dialog.Popup className="fixed top-1/2 left-1/2 z-[35] flex h-[40rem] max-h-[85vh] w-[56rem] max-w-[95vw] -translate-x-1/2 -translate-y-1/2 flex-col overflow-hidden rounded-shell-lg bg-surface shadow-elevation-3">
          <div className="flex shrink-0 items-center justify-between border-b border-border px-5 py-4">
            <Dialog.Title className="text-heading font-medium text-ink">
              Settings
            </Dialog.Title>
            <Dialog.Close
              className={iconButtonClasses()}
              aria-label="Close settings"
            >
              <X className="size-5" />
            </Dialog.Close>
          </div>
          <div className="flex flex-1 overflow-hidden">
            <nav className="w-56 shrink-0 overflow-y-auto border-e border-border py-4">
              {SETTINGS_GROUP_ORDER.map((group) => {
                const groupSections = sections.filter((section) => section.group === group);
                if (groupSections.length === 0) return null;

                return (
                  <div key={group} className="mb-4 last:mb-0">
                    <p className="px-5 pb-1 text-label-sm font-medium tracking-wide text-ink-muted uppercase">
                      {SETTINGS_GROUP_LABELS[group]}
                    </p>
                    <ul>
                      {groupSections.map((section) => (
                        <li key={section.path}>
                          <NavLink
                            to={section.path}
                            replace
                            className={({ isActive }) =>
                              `block rounded-e-full py-2 ps-5 pe-4 text-body transition-colors ${
                                isActive
                                  ? "bg-surface-hover font-medium text-ink"
                                  : "text-ink-muted hover:bg-surface-hover"
                              }`
                            }
                          >
                            {section.label}
                          </NavLink>
                        </li>
                      ))}
                    </ul>
                  </div>
                );
              })}
            </nav>
            <main className="flex-1 overflow-y-auto p-6">
              <Outlet />
            </main>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
