import { useEffect, useState } from "react";
import { useSearchParams } from "react-router";
import { Button } from "../components/ui/Button";
import { useAuthStore } from "../lib/authStore";
import { useConnectionsStore } from "../lib/connectionsStore";
import { useAsyncAction } from "../hooks/useAsyncAction";
import { errorMessage } from "../lib/errorMessage";
import { CalendarPickerModal } from "./CalendarPickerModal";
import { DisconnectConnectionModal } from "./DisconnectConnectionModal";

// The Google callback redirect (handlers.ConnectionHandler.Callback, #285)
// lands back here with one of these query params — read once on mount, then
// cleared, so a reload of this Section doesn't keep re-showing the banner.
const CONNECT_ERROR_MESSAGES: Record<string, string> = {
  declined: "Google sign-in was cancelled before it finished.",
  invalid: "That connection link is no longer valid — try connecting again.",
  failed: "Google couldn't be connected. Try again in a moment.",
};

// The Settings page's Connections Section (#285, ADR-0049, ADR-0051): a
// User authorizes a Google account and sees it listed here, distinct from
// AccountSection so a third-party grant is never confused with the User's
// own login. No Linked Calendar exists yet — that's the Calendar picker's,
// a later ticket's.
export function ConnectionsSection() {
  const user = useAuthStore((state) => state.user);
  const connections = useConnectionsStore((state) => state.connections);
  const fetchConnections = useConnectionsStore((state) => state.fetchConnections);
  const connectGoogle = useConnectionsStore((state) => state.connectGoogle);

  const [searchParams, setSearchParams] = useSearchParams();
  const [disconnectingId, setDisconnectingId] = useState<number | null>(null);
  const { isSubmitting, error, setError, run } = useAsyncAction();

  const disconnectingConnection = connections.find((c) => c.id === disconnectingId);

  // Captured once, from a lazy initializer rather than an effect, so the URL
  // clear below can't race the read: by the time any effect runs, this has
  // already been read off searchParams' mount-time value. The initializer
  // closes over searchParams as it stood on the very first render — it never
  // re-runs on a later one, however searchParams itself later changes.
  const [banner] = useState(() => {
    const connected = searchParams.get("connected");
    const connectError = searchParams.get("connect_error");
    if (connected) return { kind: "success" as const, text: "Google account connected." };
    if (connectError) {
      return {
        kind: "error" as const,
        text: CONNECT_ERROR_MESSAGES[connectError] ?? "Google couldn't be connected.",
      };
    }
    return null;
  });

  // Which Connection the Calendar picker is open for, or null. Seeded once
  // at mount from the Callback redirect's connection_id (#286) — the picker
  // opens immediately after authorizing — and set again later when a User
  // clicks "Choose calendars" on a Connection's row to re-run it (#295).
  // One piece of state, one render, so the two entry points can never stack
  // two identical dialogs.
  const [pickerConnectionId, setPickerConnectionId] = useState<number | null>(() => {
    const raw = searchParams.get("connection_id");
    if (!raw) return null;
    const parsed = Number(raw);
    return Number.isFinite(parsed) ? parsed : null;
  });

  useEffect(() => {
    fetchConnections().catch((err) => setError(errorMessage(err)));
  }, [fetchConnections, setError]);

  // Clears connected/connect_error once shown, so reloading this Section
  // later doesn't keep re-showing a banner from a round trip that's over —
  // banner itself no longer depends on the params by this point, so clearing
  // them can't make it disappear.
  useEffect(() => {
    if (banner) setSearchParams({}, { replace: true });
    // banner is captured once at mount and never changes, so this only ever
    // needs to run once too.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function handleConnect() {
    await run(async () => {
      const url = await connectGoogle();
      window.location.href = url;
    });
  }


  return (
    <section>
      <h2 className="text-heading font-medium text-ink">Connections</h2>
      <p className="mt-1 text-body text-ink-muted">
        Authorize an external account so its calendars can come in here. Separate from your own
        login above — a Connection is a third-party grant, not this account.
      </p>

      {banner && (
        <p className={`mt-4 text-label-sm ${banner.kind === "error" ? "text-danger" : "text-ink-muted"}`}>
          {banner.text}
        </p>
      )}
      {error && <p className="mt-2 text-label-sm text-danger">{error}</p>}

      <ul className="mt-4 flex flex-col gap-2">
        {connections.map((connection) => (
          <li
            key={connection.id}
            className="flex items-center justify-between rounded-md border border-border px-3 py-2"
          >
            <div>
              <p className="text-body text-ink">{connection.accountEmail}</p>
              <p className="text-label-sm text-ink-muted">
                Google ·{" "}
                {connection.status === "live"
                  ? "Connected"
                  : connection.status === "expired"
                    ? "Expired — reconnect to keep it syncing"
                    : "Revoked — reconnect to keep it syncing"}
              </p>
            </div>
            <div className="flex items-center gap-2">
              {/* Expired and revoked get the identical action (#291,
                  ADR-0075): either way the fix is the same OAuth round trip,
                  which Upsert's own ON CONFLICT reuses onto this same
                  Connection row — there's no separate flow for "reconnect a
                  revoked grant" vs. "reconnect an expired one". */}
              {connection.status !== "live" && (
                <Button onClick={handleConnect} loading={isSubmitting}>
                  Reconnect
                </Button>
              )}
              {/* The Calendar picker is re-runnable (#295): opens here for
                  adding a calendar skipped on the first pass, and — the same
                  dialog — from the Connection's heading in the sidebar. */}
              <Button
                variant="outline"
                color="secondary"
                size="small"
                onClick={() => setPickerConnectionId(connection.id)}
              >
                Choose calendars
              </Button>
              <Button
                variant="outline"
                color="secondary"
                size="small"
                onClick={() => setDisconnectingId(connection.id)}
                aria-label={`Disconnect ${connection.accountEmail}`}
              >
                Disconnect
              </Button>
            </div>
          </li>
        ))}
        {connections.length === 0 && (
          <p className="text-label-sm text-ink-muted">No accounts connected yet.</p>
        )}
      </ul>

      <div className="mt-6 border-t border-border pt-6">
        {user?.googleProviderAvailable ? (
          <>
            <p className="text-label-sm text-ink-muted">
              Google will warn that this app "hasn't been verified" — that's expected, not a
              defect: this instance's owner registered their own Google API credentials rather
              than a shared one. It's safe to continue past it.
            </p>
            <Button className="mt-3" onClick={handleConnect} loading={isSubmitting}>
              Connect a Google account
            </Button>
          </>
        ) : (
          <p className="text-label-sm text-ink-muted">
            This instance has no Google credentials configured, so connecting a Google account
            isn't available here.
          </p>
        )}
      </div>

      {pickerConnectionId !== null && (
        <CalendarPickerModal
          connectionId={pickerConnectionId}
          onClose={() => setPickerConnectionId(null)}
        />
      )}
      {disconnectingConnection && (
        <DisconnectConnectionModal
          connectionId={disconnectingConnection.id}
          accountEmail={disconnectingConnection.accountEmail}
          onClose={() => setDisconnectingId(null)}
        />
      )}
    </section>
  );
}
