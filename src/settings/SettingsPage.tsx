import { ArrowLeft } from "lucide-react";
import { NavLink, Outlet, useNavigate } from "react-router";
import { IconButton } from "../components/ui/IconButton";
import { useAuthStore } from "../lib/authStore";
import { getSettingsSections } from "./settingsSections";

// The Settings page shell (#112): a left-hand nav addresses each section by
// its own route rather than one flat scroll, so account administration
// (#109) can land as a fifth item without the page growing unbounded.
export function SettingsPage() {
  const navigate = useNavigate();
  const isAdmin = useAuthStore((state) => state.user?.isAdmin ?? false);
  const sections = getSettingsSections(isAdmin);

  return (
    <div className="flex h-screen flex-col bg-surface text-ink">
      <header className="flex h-16 shrink-0 items-center gap-4 border-b border-border bg-surface px-4">
        <IconButton
          onClick={() => navigate("/")}
          aria-label="Back to calendar"
        >
          <ArrowLeft className="size-5" />
        </IconButton>
        <span className="text-heading font-medium text-ink">Settings</span>
      </header>
      <div className="flex flex-1 overflow-hidden">
        <nav className="w-56 shrink-0 overflow-y-auto border-e border-border py-4">
          <ul>
            {sections.map((section) => (
              <li key={section.path}>
                <NavLink
                  to={section.path}
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
        </nav>
        <main className="flex-1 overflow-y-auto p-6">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
