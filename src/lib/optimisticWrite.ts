import { ApiError } from "./apiClient";
import { toast } from "./toast";

/**
 * One write that paints before the server has agreed to it, and puts the
 * cache back if it refuses (ADR-0067).
 *
 * The module owns *when* to revert and *what to say*. Each site owns *how*
 * to apply and how to undo, because the correct undo is site-specific:
 * `addEvent`'s optimistic path is fire-and-forget, so another write can
 * interleave before it fails, and restoring a whole slice would clobber that
 * other change where removing the one failed id does not.
 */
export interface OptimisticWrite {
  /** Paint the change now, before the server is asked. */
  apply: () => void;
  /** Undo `apply` — as narrowly as this site can, not by restoring a slice
   * wholesale unless that is genuinely what this site's inverse is. */
  revert: () => void;
  /** The server write. A rejection is what triggers `revert`. */
  dispatch: () => Promise<void>;
  /**
   * Ran once `dispatch` resolves, for a site with more to do than paint —
   * `updateCalendar` swapping in the masked Subscription URL the server
   * returns, rather than the raw one the User typed (#88, ADR-0032). Inside
   * the same try as `dispatch`, so a failure here reverts too.
   */
  onSuccess?: () => Promise<void> | void;
  /**
   * The Calendar this write concerns, when there is one whose Access could
   * have changed underneath the caller. Absent for a write with no Calendar
   * to name — a Notification, or the creation of the Calendar itself.
   */
  calendarId?: string;
  /** Shown when the failure is not an Access change: validation, network,
   * anything else. */
  fallbackMessage: string;
}

/**
 * Whether `error` is the shape the server uses for a write refused because
 * the caller's Access changed underneath them — the Calendar's Share was
 * revoked or downgraded (403 "forbidden") or the Event itself is gone (404
 * "not found") — as opposed to validation or network failure, which keep the
 * site's own generic message (#116).
 */
function isAccessChangeError(error: unknown): boolean {
  return (
    error instanceof ApiError && (error.status === 403 || error.status === 404)
  );
}

/**
 * Binds the Access-change policy once per store, so the twelve call sites
 * state only what differs between them.
 *
 * `onAccessChange` names the affected Calendar and refetches Calendars,
 * returning the message to show (#116). It is a parameter rather than an
 * import because this module would otherwise have to reach into
 * `calendarsStore` for it, and `calendarsStore` is one of its own callers.
 * Optional because one caller genuinely has no Calendar to name:
 * `notificationsStore` binds none, which is what makes the seam real rather
 * than a claim.
 *
 * Returns whether the write landed. Callers that cascade other local state
 * off a write (the Calendar cascades) read it; the rest may ignore it.
 */
export function makeOptimisticWrite(
  onAccessChange?: (calendarId: string) => Promise<string>,
): (write: OptimisticWrite) => Promise<boolean> {
  return async function optimisticWrite(write: OptimisticWrite) {
    write.apply();

    try {
      await write.dispatch();
      await write.onSuccess?.();
      return true;
    } catch (error) {
      write.revert();
      if (write.calendarId && onAccessChange && isAccessChangeError(error)) {
        toast.error(await onAccessChange(write.calendarId));
      } else {
        toast.error(write.fallbackMessage);
      }
      return false;
    }
  };
}
