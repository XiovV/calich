import { authedFetch, errorFromResponse } from "./apiClient";

// Availability Schedules (#320, ADR-0085): a named weekly pattern of time
// ranges belonging to one User, carrying its own IANA timezone. Unlike
// Calendar Sets and Task Lists, a Schedule carries no Workspace of its own —
// it is reusable across every Workspace a Booking Link might write into —
// so these calls carry no workspaceHeaders.

export interface AvailabilityRange {
  // 0-6, Sunday-Saturday, matching the Week start Preference's own
  // convention.
  weekday: number;
  startMinute: number;
  endMinute: number;
}

export interface AvailabilitySchedule {
  id: number;
  name: string;
  tzid: string;
  ranges: AvailabilityRange[];
}

export const availabilitySchedulesApi = {
  // Every Schedule the caller owns. The backend seeds a "Default" one the
  // first time this returns none — tzHint, the caller's own
  // browser-detected IANA zone, seeds that Default's timezone the one time
  // it doesn't exist yet, and is ignored otherwise.
  async list(accessToken: string, tzHint?: string): Promise<AvailabilitySchedule[]> {
    const query = tzHint ? `?tz=${encodeURIComponent(tzHint)}` : "";
    const response = await authedFetch(accessToken, `/api/availability-schedules/${query}`, {
      credentials: "include",
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as AvailabilitySchedule[];
  },

  // Creates a new Schedule. ranges may be empty — an empty Schedule is
  // legal (ADR-0085).
  async create(accessToken: string, name: string, tzid: string, ranges: AvailabilityRange[]): Promise<AvailabilitySchedule> {
    const response = await authedFetch(accessToken, "/api/availability-schedules/", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name, tzid, ranges }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as AvailabilitySchedule;
  },

  // Replaces id's name, timezone and entire weekly range set at once.
  async update(
    accessToken: string,
    id: number,
    name: string,
    tzid: string,
    ranges: AvailabilityRange[],
  ): Promise<AvailabilitySchedule> {
    const response = await authedFetch(accessToken, `/api/availability-schedules/${id}`, {
      method: "PATCH",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name, tzid, ranges }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as AvailabilitySchedule;
  },

  // Deletes id outright.
  async remove(accessToken: string, id: number): Promise<void> {
    const response = await authedFetch(accessToken, `/api/availability-schedules/${id}`, {
      method: "DELETE",
      credentials: "include",
    });
    if (!response.ok) throw await errorFromResponse(response);
  },
};
