import { authHeader, errorFromResponse } from "./apiClient";
import type { ReminderChannel } from "./event";

// ReminderOverride is a User's personal Reminder override on an Event (#105,
// #117, ADR-0036): one offset, one Channel, one muted flag, substituted into
// every one of the Event's Reminders wherever it sets a value — a modifier,
// never a personal replacement list. offsetMinutes and channel are
// independently undefined when that dimension isn't overridden, in which
// case the Event's own Reminders apply for it.
export interface ReminderOverride {
  offsetMinutes?: number;
  channel?: ReminderChannel;
  muted: boolean;
}

// The wire shape omits offsetMinutes, channel, and (since it's a bool) muted
// itself when unset, so a caller who never set an override gets back `{}`.
interface ReminderOverrideWire {
  offsetMinutes?: number;
  channel?: ReminderChannel;
  muted?: boolean;
}

function fromWire(wire: ReminderOverrideWire): ReminderOverride {
  return { offsetMinutes: wire.offsetMinutes, channel: wire.channel, muted: wire.muted ?? false };
}

// hasReminderOverride reports whether override actually overrides anything —
// false for the zero-value override get returns when the caller has never
// set one, in which case the Event's own Reminders apply unchanged.
export function hasReminderOverride(override: ReminderOverride): boolean {
  return override.muted || override.offsetMinutes !== undefined || override.channel !== undefined;
}

export const reminderOverrideApi = {
  // get returns the caller's own override on eventId, or the unset override
  // (every field undefined, muted false) if they've never set one — the
  // Event's own Reminders apply unchanged in that case. Any caller with at
  // least Viewer Access to the Event's Calendar may call this.
  async get(accessToken: string, eventId: string): Promise<ReminderOverride> {
    const response = await fetch(`/api/events/${eventId}/reminder-override`, {
      credentials: "include",
      headers: authHeader(accessToken),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return fromWire((await response.json()) as ReminderOverrideWire);
  },

  // set replaces the caller's override on eventId wholesale.
  async set(
    accessToken: string,
    eventId: string,
    override: ReminderOverride,
  ): Promise<ReminderOverride> {
    const response = await fetch(`/api/events/${eventId}/reminder-override`, {
      method: "PUT",
      credentials: "include",
      headers: { "Content-Type": "application/json", ...authHeader(accessToken) },
      body: JSON.stringify(override),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return fromWire((await response.json()) as ReminderOverrideWire);
  },

  // clear removes the caller's override on eventId, falling back to the
  // Event's own Reminders. A no-op if they never set one.
  async clear(accessToken: string, eventId: string): Promise<void> {
    const response = await fetch(`/api/events/${eventId}/reminder-override`, {
      method: "DELETE",
      credentials: "include",
      headers: authHeader(accessToken),
    });
    if (!response.ok) throw await errorFromResponse(response);
  },
};
