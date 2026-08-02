import { Menu } from "@base-ui/react/menu";
import { CircleUserRound, LogOut } from "lucide-react";
import { useNavigate } from "react-router";
import { useAuthStore } from "../lib/authStore";

export function UserMenu() {
  const session = useAuthStore((state) => state.session);
  const logout = useAuthStore((state) => state.logout);
  const navigate = useNavigate();

  if (!session) return null;

  function handleLogout() {
    logout();
    navigate("/login", { replace: true });
  }

  return (
    <Menu.Root>
      <Menu.Trigger
        aria-label="Account menu"
        className="rounded-shell-pill p-1.5 text-ink-muted hover:bg-surface-hover"
      >
        <CircleUserRound className="size-5" />
      </Menu.Trigger>
      <Menu.Portal>
        <Menu.Positioner sideOffset={4} align="end">
          <Menu.Popup className="rounded-shell-md border border-border bg-surface py-1 shadow-elevation-2">
            <div className="px-3 py-1.5 text-label-sm text-ink-muted">
              {session.user.email}
            </div>
            <Menu.Item
              onClick={handleLogout}
              className="flex cursor-default items-center gap-2 px-3 py-1.5 text-body text-ink data-[highlighted]:bg-surface-hover"
            >
              <LogOut className="size-4" />
              Logout
            </Menu.Item>
          </Menu.Popup>
        </Menu.Positioner>
      </Menu.Portal>
    </Menu.Root>
  );
}
