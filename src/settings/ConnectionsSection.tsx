import { useEffect, useState } from "react";
import { useSearchParams } from "react-router";
import { Trash2 } from "lucide-react";
import { Button } from "../components/ui/Button";
import { IconButton } from "../components/ui/IconButton";
import { useAuthStore } from "../lib/authStore";
import { useConnectionsStore } from "../lib/connectionsStore";
import { useAsyncAction } from "../hooks/useAsyncAction";
import { errorMessage } from "../lib/errorMessage";

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
  const disconnect = useConnectionsStore((state) => state.disconnect);

  const [searchParams, setSearchParams] = useSearchParams();
  const [disconnectingId, setDisconnectingId] = useState<number | null>(null);
  const { isSubmitting, error, setError, run } = useAsyncAction();

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

  async function handleDisconnect(id: number, accountEmail: string) {
    if (!window.confirm(`Disconnect ${accountEmail}?`)) return;

    setDisconnectingId(id);
    setError(null);
    try {
      await disconnect(id);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setDisconnectingId(null);
    }
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
            <IconButton
              onClick={() => handleDisconnect(connection.id, connection.accountEmail)}
              disabled={disconnectingId === connection.id}
              aria-label={`Disconnect ${connection.accountEmail}`}
            >
              <Trash2 className="size-4" />
            </IconButton>
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
    </section>
  );
}
