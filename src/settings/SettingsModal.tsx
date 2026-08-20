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
//
// The rail is deliberately `surface-sunken` and deliberately border-less,
// against ADR-0014's flat chrome — please don't flatten it back. A floating
// two-column panel has no page edges to give its columns structure, so the
// tint does that job, and a divider on top of the tint would draw a line
// straight between the active item and the pane it is meant to continue
// into: the active item is `bg-surface`, the content pane's own colour, so
// that the selection reads as the panel showing through. Rail / hover /
// active are three distinct steps (100/200/white, flipping to 900/800/black),
// which is why active no longer shares `surface-hover` with hover. Scoped to
// this dialog on purpose: the AppShell sidebar stays flat on `surface`.
//
// There is no header bar. No other dialog here has one — they all sit their
// Dialog.Title in the popup's own padding — so "Settings" lives at the rail's
// top in the same type, still the dialog's accessible name (ADR-0049), and
// the ✕ is the popup's first child so it stays first in the tab order rather
// than sitting behind all seven rail links. That makes it a positioned overlay,
// so the content column reserves its footprint with a spacer (h-13 = the pt-4
// inset plus the size-9 button) and nothing ever scrolls underneath it.
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
        <Dialog.Popup className="fixed top-1/2 left-1/2 z-[35] flex h-[40rem] max-h-[85vh] w-[56rem] max-w-[95vw] -translate-x-1/2 -translate-y-1/2 overflow-hidden rounded-shell-lg bg-surface shadow-elevation-3">
          <Dialog.Close
            className={iconButtonClasses({ className: "absolute top-4 right-4 z-10" })}
            aria-label="Close settings"
            title="Close settings"
          >
            <X className="size-5" />
          </Dialog.Close>
          <div className="flex w-56 shrink-0 flex-col bg-surface-sunken">
            <Dialog.Title className="shrink-0 px-5 pt-5 pb-6 text-heading font-medium text-ink">
              Settings
            </Dialog.Title>
            <nav className="flex-1 overflow-y-auto pb-4">
              {SETTINGS_GROUP_ORDER.map((group) => {
                const groupSections = sections.filter((section) => section.group === group);
                if (groupSections.length === 0) return null;

                return (
                  <div key={group} className="mb-8 last:mb-0">
                    <p className="px-5 pb-2 text-label-sm font-medium tracking-wide text-ink-muted uppercase">
                      {SETTINGS_GROUP_LABELS[group]}
                    </p>
                    <ul>
                      {groupSections.map((section) => (
                        <li key={section.path}>
                          <NavLink
                            to={section.path}
                            replace
                            className={({ isActive }) =>
                              `flex items-center gap-2 rounded-e-full py-2 ps-5 pe-4 text-body transition-colors ${
                                isActive
                                  ? "bg-surface font-medium text-ink"
                                  : "text-ink-muted hover:bg-surface-hover"
                              }`
                            }
                          >
                            <section.icon aria-hidden className="size-4" />
                            {section.label}
                          </NavLink>
                        </li>
                      ))}
                    </ul>
                  </div>
                );
              })}
            </nav>
          </div>
          <div className="flex flex-1 flex-col overflow-hidden">
            <div aria-hidden className="h-13 shrink-0" />
            <main className="flex-1 overflow-y-auto px-6 pt-2 pb-6">
              <Outlet />
            </main>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
