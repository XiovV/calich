import { useEffect, useState } from "react";
import type { ReminderChannel } from "../lib/event";
import {
  hasReminderOverride,
  reminderOverrideApi,
  type ReminderOverride,
} from "../lib/reminderOverrideApi";
import { useAuthStore } from "../lib/authStore";
import { errorMessage } from "../lib/errorMessage";
import { toast } from "../lib/toast";
import {
  REMINDER_OFFSET_UNIT_OPTIONS,
  normalizeCustomOffset,
  reminderChannelOptions,
  splitCustomOffset,
  type ReminderOffsetUnit,
} from "../lib/reminderOffset";
import { Checkbox } from "../components/ui/Checkbox";
import { Select } from "../components/ui/Select";
import { Input } from "../components/ui/Input";
import { Button } from "../components/ui/Button";
import { fieldLabelClass } from "../components/ui/fieldStyles";

interface ReminderOverrideControlProps {
  // The Event this override applies to — an override is scoped to the
  // Event, never the Occurrence (ADR-0036), so a recurring series shares one.
  eventId: string;
  // Whether the Email Channel can be offered — the user has an account email
  // set *and* the self-hoster has SMTP configured (ADR-0021), same as
  // ReminderRow's prop of the same name.
  emailAvailable: boolean;
}

// ReminderOverrideControl is the personal Reminder override surface inside
// EventModal (#117, ADR-0036): a modifier — one offset, one Channel, one
// muted flag, substituted into every one of the Event's Reminders for the
// caller alone — never presented as an editable copy of the Event's own
// Reminder list, since the schema can't express that. Any caller with at
// least Viewer Access to the Event's Calendar may use it, independently of
// whether they may edit the Event itself; see EventModal for when this
// renders at all (#111, #117).
export function ReminderOverrideControl({
  eventId,
  emailAvailable,
}: ReminderOverrideControlProps) {
  const accessToken = useAuthStore((state) => state.accessToken);

  // null while loading; once loaded, mirrors the server's own override so
  // hasOverride below reflects what's actually saved, not the draft fields.
  const [override, setOverride] = useState<ReminderOverride | null>(null);
  const [muted, setMuted] = useState(false);
  const [offsetEnabled, setOffsetEnabled] = useState(false);
  const [amount, setAmount] = useState(10);
  const [unit, setUnit] = useState<ReminderOffsetUnit>("minutes");
  const [channelEnabled, setChannelEnabled] = useState(false);
  const [channel, setChannel] = useState<ReminderChannel>("notification");
  const [isSaving, setIsSaving] = useState(false);

  useEffect(() => {
    if (!accessToken) return;
    let cancelled = false;
    (async () => {
      try {
        const fetched = await reminderOverrideApi.get(accessToken, eventId);
        if (!cancelled) applyOverride(fetched);
      } catch {
        if (!cancelled) toast.error("Failed to load your reminder setting.");
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [accessToken, eventId]);

  function applyOverride(value: ReminderOverride) {
    setOverride(value);
    setMuted(value.muted);
    setOffsetEnabled(value.offsetMinutes !== undefined);
    if (value.offsetMinutes !== undefined) {
      const split = splitCustomOffset(value.offsetMinutes);
      setAmount(split.amount);
      setUnit(split.unit);
    }
    setChannelEnabled(value.channel !== undefined);
    if (value.channel !== undefined) setChannel(value.channel);
  }

  const hasOverride = Boolean(override && hasReminderOverride(override));

  async function handleSave() {
    if (!accessToken) return;
    setIsSaving(true);
    try {
      const saved = await reminderOverrideApi.set(accessToken, eventId, {
        offsetMinutes: offsetEnabled ? normalizeCustomOffset(amount, unit) : undefined,
        channel: channelEnabled ? channel : undefined,
        muted,
      });
      applyOverride(saved);
      toast.success("Your reminder setting was saved.");
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setIsSaving(false);
    }
  }

  async function handleClear() {
    if (!accessToken) return;
    setIsSaving(true);
    try {
      await reminderOverrideApi.clear(accessToken, eventId);
      applyOverride({ muted: false });
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setIsSaving(false);
    }
  }

  if (override === null) {
    return (
      <div className="mt-4 border-t border-border pt-4">
        <p className={fieldLabelClass}>Your reminder</p>
        <p className="mt-1.5 text-label-sm text-ink-muted">Loading…</p>
      </div>
    );
  }

  return (
    <div className="mt-4 border-t border-border pt-4">
      <p className={fieldLabelClass}>Your reminder</p>
      <p className="mt-1 text-label-sm text-ink-muted">
        Retune this event's reminders for yourself, without changing them for anyone else.
      </p>

      <label className="mt-2.5 flex items-center gap-2">
        <Checkbox checked={muted} onCheckedChange={setMuted} aria-label="Mute reminders for me" />
        <span className="text-body text-ink">Mute for me</span>
      </label>

      <label className="mt-2 flex items-center gap-2">
        <Checkbox
          checked={offsetEnabled}
          onCheckedChange={setOffsetEnabled}
          aria-label="Remind me at a different time"
        />
        <span className="text-body text-ink">Remind me at a different time</span>
      </label>
      {offsetEnabled && (
        <div className="mt-1.5 flex items-end gap-2 pl-6">
          <Input
            aria-label="Your reminder offset amount"
            type="number"
            min={0}
            value={amount}
            onChange={(event) => setAmount(Number(event.target.value))}
            className="w-16 shrink-0"
          />
          <Select
            aria-label="Your reminder offset unit"
            value={unit}
            onValueChange={setUnit}
            options={REMINDER_OFFSET_UNIT_OPTIONS}
            className="min-w-0 shrink truncate"
          />
        </div>
      )}

      <label className="mt-2 flex items-center gap-2">
        <Checkbox
          checked={channelEnabled}
          onCheckedChange={setChannelEnabled}
          aria-label="Notify me on a different channel"
        />
        <span className="text-body text-ink">Notify me on a different channel</span>
      </label>
      {channelEnabled && (
        <div className="mt-1.5 pl-6">
          <Select
            aria-label="Your reminder channel"
            value={channel}
            onValueChange={setChannel}
            options={reminderChannelOptions(emailAvailable)}
            className="min-w-0 shrink truncate"
          />
        </div>
      )}

      <div className="mt-3 flex gap-2">
        <Button size="small" onClick={handleSave} disabled={isSaving} loading={isSaving}>
          Save
        </Button>
        {hasOverride && (
          <Button
            variant="outline"
            color="secondary"
            size="small"
            onClick={handleClear}
            disabled={isSaving}
          >
            Use event reminders
          </Button>
        )}
      </div>
    </div>
  );
}
