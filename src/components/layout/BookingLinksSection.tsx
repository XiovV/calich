import { useEffect, useState } from "react";
import { Menu } from "@base-ui/react/menu";
import { Copy, MoreVertical, Plus } from "lucide-react";
import { IconButton } from "../ui/IconButton";
import { iconButtonClasses } from "../ui/iconButtonClasses";
import { useAuthStore } from "../../lib/authStore";
import { useBookingLinksStore } from "../../lib/bookingLinksStore";
import { useWorkspacesStore } from "../../lib/workspacesStore";
import { errorMessage } from "../../lib/errorMessage";
import { toast } from "../../lib/toast";
import type { BookingLink } from "../../lib/bookingLinksApi";
import { BookingLinkModal } from "./BookingLinkModal";
import { DeleteBookingLinkConfirmation } from "./DeleteBookingLinkConfirmation";

const menuItemClasses =
  "flex cursor-default items-center px-3 py-1.5 text-body text-ink data-[highlighted]:bg-surface-hover data-[disabled]:pointer-events-none data-[disabled]:opacity-50";
const destructiveMenuItemClasses =
  "flex cursor-default items-center px-3 py-1.5 text-body text-danger data-[highlighted]:bg-danger-50";

function formatDuration(minutes: number): string {
  if (minutes % 60 === 0) return `${minutes / 60}h`;
  if (minutes < 60) return `${minutes}m`;
  return `${Math.floor(minutes / 60)}h ${minutes % 60}m`;
}

const VISIBILITY_LABELS: Record<BookingLink["visibility"], string> = {
  public: "Public",
  private: "Private",
  paused: "Paused",
};

// The Booking links sidebar section (#322, ADR-0084, ADR-0087), below the
// Calendar list. Lists the Active Workspace's own links only — a Booking
// Link is scoped (User, Workspace) on the same terms as a Task List, and
// dies with the membership that gave it a Calendar to write into. Nothing
// here is public yet (#324): the copy button copies a URL that doesn't
// resolve, which is deliberate — see BookingLinkModal's own preview.
export function BookingLinksSection() {
  const user = useAuthStore((state) => state.user);
  const activeWorkspaceId = useWorkspacesStore((state) => state.activeWorkspaceId);
  const bookingLinks = useBookingLinksStore((state) => state.bookingLinks);
  const fetchBookingLinks = useBookingLinksStore((state) => state.fetchBookingLinks);
  const duplicateBookingLink = useBookingLinksStore((state) => state.duplicateBookingLink);

  const [loadError, setLoadError] = useState<string | null>(null);
  const [isCreating, setIsCreating] = useState(false);
  const [editingTarget, setEditingTarget] = useState<BookingLink | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<BookingLink | null>(null);

  useEffect(() => {
    if (activeWorkspaceId === null) return;
    fetchBookingLinks()
      .then(() => setLoadError(null))
      .catch((err) => setLoadError(errorMessage(err)));
  }, [activeWorkspaceId, fetchBookingLinks]);

  function urlFor(link: BookingLink): string {
    const handle = user?.handle ?? "";
    return `${window.location.origin}/${handle}/${link.slug}`;
  }

  async function handleCopy(link: BookingLink) {
    try {
      await navigator.clipboard.writeText(urlFor(link));
      toast.success("Link copied.");
    } catch {
      toast.error("Failed to copy link.");
    }
  }

  async function handleDuplicate(link: BookingLink) {
    try {
      await duplicateBookingLink(link.id);
    } catch (err) {
      toast.error(errorMessage(err));
    }
  }

  return (
    <div>
      <div className="flex items-center justify-between py-2 ps-5 pe-2">
        <p className="text-label-sm font-medium text-ink-muted">Booking links</p>
        <IconButton size="tiny" onClick={() => setIsCreating(true)} aria-label="New booking link">
          <Plus className="size-4" />
        </IconButton>
      </div>

      {loadError && <p className="px-5 text-label-sm text-danger">{loadError}</p>}

      <ul>
        {bookingLinks.map((link) => (
          <li
            key={link.id}
            className="group flex items-center gap-2 rounded-e-full py-2 ps-5 pe-2 transition-colors hover:bg-surface-hover"
          >
            <span className="flex min-w-0 flex-1 flex-col">
              <span className="min-w-0 truncate text-body text-ink">{link.title}</span>
              <span className="truncate text-label-sm text-ink-muted">
                {formatDuration(link.durationMinutes)} · {VISIBILITY_LABELS[link.visibility]}
              </span>
            </span>

            <IconButton
              size="tiny"
              onClick={() => handleCopy(link)}
              aria-label={`Copy ${link.title}'s link`}
              title={`Copy ${link.title}'s link`}
              className="opacity-0 focus-visible:opacity-100 group-hover:opacity-100"
            >
              <Copy className="size-3.5" />
            </IconButton>

            <Menu.Root>
              <Menu.Trigger
                aria-label={`${link.title} actions`}
                title={`${link.title} actions`}
                className={iconButtonClasses({
                  size: "tiny",
                  className:
                    "opacity-0 focus-visible:opacity-100 group-hover:opacity-100 data-[popup-open]:opacity-100",
                })}
              >
                <MoreVertical className="size-3.5" />
              </Menu.Trigger>
              <Menu.Portal>
                <Menu.Positioner sideOffset={4} align="end" className="z-[60]">
                  <Menu.Popup className="rounded-shell-md border border-border bg-surface py-1 shadow-elevation-2">
                    <Menu.Item onClick={() => setEditingTarget(link)} className={menuItemClasses}>
                      Edit
                    </Menu.Item>
                    <Menu.Item onClick={() => handleDuplicate(link)} className={menuItemClasses}>
                      Duplicate
                    </Menu.Item>
                    <div role="separator" className="my-1 border-t border-border" />
                    <Menu.Item onClick={() => setDeleteTarget(link)} className={destructiveMenuItemClasses}>
                      Delete
                    </Menu.Item>
                  </Menu.Popup>
                </Menu.Positioner>
              </Menu.Portal>
            </Menu.Root>
          </li>
        ))}

        {bookingLinks.length === 0 && !loadError && (
          <p className="px-5 text-label-sm text-ink-muted">No booking links yet.</p>
        )}
      </ul>

      {isCreating && <BookingLinkModal mode="create" onClose={() => setIsCreating(false)} />}
      {editingTarget && (
        <BookingLinkModal mode="edit" bookingLink={editingTarget} onClose={() => setEditingTarget(null)} />
      )}
      {deleteTarget && (
        <DeleteBookingLinkConfirmation bookingLink={deleteTarget} onClose={() => setDeleteTarget(null)} />
      )}
    </div>
  );
}
