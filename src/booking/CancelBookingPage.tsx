import { useState } from "react";
import type { ReactNode } from "react";
import { useSearchParams } from "react-router";
import { Button } from "../components/ui/Button";
import { publicBookingApi, isBookingAlreadyStartedError, isInvalidCancelTokenError } from "../lib/publicBookingApi";

// The signed cancel link's own destination (#327, ADR-0087): a stranger with
// no account and no Session follows a link from their booking confirmation
// mail to /cancel-booking?token=..., routed in App.tsx outside
// ProtectedRoute, sibling to the public Booking Link page. Cancelling is a
// deliberate click, never the bare GET that opened this page — a mail
// client or link scanner prefetching the page can't trigger it.

type Status = "confirming" | "cancelling" | "cancelled" | "already-started" | "invalid" | "error";

export function CancelBookingPage() {
  const [searchParams] = useSearchParams();
  const token = searchParams.get("token") ?? "";
  const [status, setStatus] = useState<Status>(token ? "confirming" : "invalid");

  async function handleCancel() {
    setStatus("cancelling");
    try {
      await publicBookingApi.cancel(token);
      setStatus("cancelled");
    } catch (error) {
      if (isBookingAlreadyStartedError(error)) {
        setStatus("already-started");
      } else if (isInvalidCancelTokenError(error)) {
        setStatus("invalid");
      } else {
        setStatus("error");
      }
    }
  }

  if (status === "cancelled") {
    return (
      <PageShell>
        <h1 className="text-heading-lg font-medium text-ink">Booking cancelled</h1>
        <p className="mt-2 text-body text-ink-muted">
          The time has been freed up. The host has been notified.
        </p>
      </PageShell>
    );
  }

  if (status === "already-started") {
    return (
      <PageShell>
        <h1 className="text-heading-lg font-medium text-ink">This booking has already started</h1>
        <p className="mt-2 text-body text-ink-muted">It's too late to cancel this one.</p>
      </PageShell>
    );
  }

  if (status === "invalid") {
    return (
      <PageShell>
        <h1 className="text-heading-lg font-medium text-ink">This cancel link isn't valid</h1>
        <p className="mt-2 text-body text-ink-muted">
          It may have been mistyped, or the booking it points to is already gone.
        </p>
      </PageShell>
    );
  }

  if (status === "error") {
    return (
      <PageShell>
        <h1 className="text-heading-lg font-medium text-ink">Something went wrong</h1>
        <p className="mt-2 text-body text-ink-muted">Please try again in a moment.</p>
      </PageShell>
    );
  }

  return (
    <PageShell>
      <h1 className="text-heading-lg font-medium text-ink">Cancel this booking?</h1>
      <p className="mt-2 text-body text-ink-muted">
        This frees up the time immediately and lets the host know.
      </p>
      <Button className="mt-6" variant="filled" disabled={status === "cancelling"} onClick={handleCancel}>
        {status === "cancelling" ? "Cancelling…" : "Cancel booking"}
      </Button>
    </PageShell>
  );
}

function PageShell({ children }: { children: ReactNode }) {
  return (
    <div className="min-h-screen bg-surface px-4 py-10 sm:px-8">
      <div className="mx-auto max-w-3xl">{children}</div>
    </div>
  );
}
