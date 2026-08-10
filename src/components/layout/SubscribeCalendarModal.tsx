import { useState, type KeyboardEvent } from "react";
import { Dialog } from "@base-ui/react/dialog";
import { format } from "date-fns";
import { calendarsApi, type SubscriptionPreview } from "../../lib/calendarsApi";
import { useCalendarsStore } from "../../lib/calendarsStore";
import { useAuthStore } from "../../lib/authStore";
import { useShellStore } from "../../lib/shellStore";
import { errorMessage } from "../../lib/errorMessage";
import { getNextUnusedColor } from "../../lib/calendarColors";
import { ColorSwatchPicker } from "./ColorSwatchPicker";
import { Button } from "../ui/Button";
import { buttonClasses } from "../ui/buttonClasses";
import { Checkbox } from "../ui/Checkbox";
import { Input } from "../ui/Input";
import { fieldLabelClass } from "../ui/fieldStyles";

interface SubscribeCalendarModalProps {
  onClose: () => void;
}

// Best-effort default name when the feed supplies no X-WR-CALNAME — the
// URL's host, since (unlike file import) there's no filename to fall back
// to.
function hostnameOf(url: string): string {
  try {
    return new URL(url).hostname;
  } catch {
    return "";
  }
}

function formatRangeDate(iso: string): string {
  return format(new Date(iso), "MMM d, yyyy");
}

export function SubscribeCalendarModal({ onClose }: SubscribeCalendarModalProps) {
  const accessToken = useAuthStore((state) => state.accessToken);
  const calendars = useCalendarsStore((state) => state.calendars);
  const subscribeCalendar = useCalendarsStore((state) => state.subscribeCalendar);
  const addCheckedCalendarId = useShellStore(
    (state) => state.addCheckedCalendarId,
  );

  const [url, setUrl] = useState("");
  const [preview, setPreview] = useState<SubscriptionPreview | null>(null);
  const [name, setName] = useState("");
  const [color, setColor] = useState(() =>
    getNextUnusedColor(calendars.map((calendar) => calendar.color)),
  );
  const [isPreviewing, setIsPreviewing] = useState(false);
  const [isSubscribing, setIsSubscribing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // keepAlarms defaults off (#87, ADR-0032): the disclosure guard that made
  // faithful alarm import right for a one-time ICS import doesn't survive
  // an automatic, repeated Refresh, so this checkbox restores the missing
  // consent moment for a Subscription.
  const [keepAlarms, setKeepAlarms] = useState(false);

  const canPreview = url.trim() !== "" && !isPreviewing;
  const canSubscribe = preview !== null && name.trim() !== "" && !isSubscribing;

  async function handlePreview() {
    if (!accessToken || !canPreview) return;
    setIsPreviewing(true);
    setError(null);
    try {
      const result = await calendarsApi.previewSubscription(accessToken, url.trim());
      setPreview(result);
      setName(result.name || hostnameOf(url.trim()));
      setColor(result.color);
    } catch (err) {
      setPreview(null);
      setError(errorMessage(err));
    } finally {
      setIsPreviewing(false);
    }
  }

  async function handleSubscribe() {
    if (!canSubscribe) return;
    setIsSubscribing(true);
    setError(null);
    try {
      const calendar = await subscribeCalendar(
        url.trim(),
        name.trim(),
        color,
        keepAlarms,
      );
      addCheckedCalendarId(calendar.id);
      onClose();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setIsSubscribing(false);
    }
  }

  function handleEnterToSave(event: KeyboardEvent) {
    const target = event.target as HTMLElement;
    if (
      event.key === "Enter" &&
      !event.shiftKey &&
      target.tagName !== "BUTTON" &&
      target.tagName !== "TEXTAREA"
    ) {
      event.preventDefault();
      if (preview) {
        handleSubscribe();
      } else {
        handlePreview();
      }
    }
  }

  return (
    <Dialog.Root
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <Dialog.Portal>
        <Dialog.Backdrop className="fixed inset-0 z-40 bg-ink/20" />
        <Dialog.Popup
          onKeyDown={handleEnterToSave}
          className="fixed top-1/2 left-1/2 z-50 w-96 -translate-x-1/2 -translate-y-1/2 rounded-shell-lg bg-surface p-5 shadow-elevation-3"
        >
          <Dialog.Title className="text-heading font-medium text-ink">
            Subscribe to a calendar
          </Dialog.Title>

          <form
            onSubmit={(event) => {
              event.preventDefault();
              if (preview) {
                handleSubscribe();
              } else {
                handlePreview();
              }
            }}
          >
            <Input
              label="Calendar URL"
              value={url}
              onChange={(event) => {
                setUrl(event.target.value);
                setPreview(null);
              }}
              placeholder="https://example.com/calendar.ics"
              className="mt-4"
              autoFocus
            />

            {error && (
              <p className="mt-2 text-label-sm text-danger" role="alert">
                {error}
              </p>
            )}

            {preview && (
              <>
                <p className="mt-3 text-label-sm text-ink-muted">
                  {preview.eventCount} event{preview.eventCount === 1 ? "" : "s"}
                  {preview.rangeStart && preview.rangeEnd
                    ? ` from ${formatRangeDate(preview.rangeStart)} to ${formatRangeDate(preview.rangeEnd)}`
                    : ""}
                </p>

                <Input
                  label="Name"
                  value={name}
                  onChange={(event) => setName(event.target.value)}
                  placeholder="Calendar name"
                  className="mt-4"
                />

                <div className="mt-4">
                  <p className={fieldLabelClass}>Color</p>
                  <div className="mt-2">
                    <ColorSwatchPicker value={color} onValueChange={setColor} />
                  </div>
                </div>

                <label className="mt-4 flex items-start gap-2 text-label-sm text-ink">
                  <Checkbox
                    checked={keepAlarms}
                    onCheckedChange={setKeepAlarms}
                    aria-label="Keep this feed's reminders"
                  />
                  <span>
                    Keep this feed&apos;s reminders, including email reminders
                    this instance will send
                  </span>
                </label>
              </>
            )}

            <div className="mt-5 flex justify-end gap-2">
              <Dialog.Close
                className={buttonClasses({
                  variant: "outline",
                  color: "secondary",
                  size: "small",
                })}
              >
                Cancel
              </Dialog.Close>
              {preview ? (
                <Button
                  type="submit"
                  size="small"
                  disabled={!canSubscribe}
                  loading={isSubscribing}
                >
                  Subscribe
                </Button>
              ) : (
                <Button
                  type="submit"
                  size="small"
                  disabled={!canPreview}
                  loading={isPreviewing}
                >
                  Preview
                </Button>
              )}
            </div>
          </form>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
