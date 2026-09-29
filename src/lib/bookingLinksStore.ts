import { create } from "zustand";
import { useAuthStore } from "./authStore";
import { type BookingLink, type BookingLinkWrite, bookingLinksApi } from "./bookingLinksApi";

// The Booking links sidebar section's data source (#322, ADR-0084,
// ADR-0087). Mirrors taskListsStore's shape — a Booking Link is scoped
// (User, Workspace) on the same terms as a Task List.
interface BookingLinksState {
  bookingLinks: BookingLink[];
  fetchBookingLinks: () => Promise<void>;
  createBookingLink: (write: BookingLinkWrite) => Promise<BookingLink>;
  updateBookingLink: (id: number, write: BookingLinkWrite) => Promise<BookingLink>;
  deleteBookingLink: (id: number) => Promise<void>;
  duplicateBookingLink: (id: number) => Promise<BookingLink>;
  addConflictCalendar: (id: number, calendarId: string) => Promise<void>;
  removeConflictCalendar: (id: number, calendarId: string) => Promise<void>;
}

function requireAccessToken(): string {
  const accessToken = useAuthStore.getState().accessToken;
  if (!accessToken) throw new Error("Not authenticated.");
  return accessToken;
}

export const useBookingLinksStore = create<BookingLinksState>((set, get) => ({
  bookingLinks: [],

  fetchBookingLinks: async () => {
    const bookingLinks = await bookingLinksApi.list(requireAccessToken());
    set({ bookingLinks });
  },

  createBookingLink: async (write) => {
    const created = await bookingLinksApi.create(requireAccessToken(), write);
    set({ bookingLinks: [...get().bookingLinks, created] });
    // The first Booking Link a User creates claims their Handle for them
    // (ADR-0084) — this session's own authStore.user wouldn't otherwise
    // learn about it until its next unrelated refresh.
    useAuthStore.getState().refreshUser().catch(() => {});
    return created;
  },

  updateBookingLink: async (id, write) => {
    const updated = await bookingLinksApi.update(requireAccessToken(), id, write);
    set({ bookingLinks: get().bookingLinks.map((l) => (l.id === id ? updated : l)) });
    return updated;
  },

  deleteBookingLink: async (id) => {
    await bookingLinksApi.remove(requireAccessToken(), id);
    set({ bookingLinks: get().bookingLinks.filter((l) => l.id !== id) });
  },

  duplicateBookingLink: async (id) => {
    const duplicated = await bookingLinksApi.duplicate(requireAccessToken(), id);
    set({ bookingLinks: [...get().bookingLinks, duplicated] });
    useAuthStore.getState().refreshUser().catch(() => {});
    return duplicated;
  },

  // Both immediate-effect, no save step, mirroring calendarSetsStore's own
  // addCalendarToSet/removeCalendarFromSet.
  addConflictCalendar: async (id, calendarId) => {
    await bookingLinksApi.addConflictCalendar(requireAccessToken(), id, calendarId);
    set({
      bookingLinks: get().bookingLinks.map((l) =>
        l.id === id && !l.conflictCalendarIds.includes(calendarId)
          ? { ...l, conflictCalendarIds: [...l.conflictCalendarIds, calendarId] }
          : l,
      ),
    });
  },

  removeConflictCalendar: async (id, calendarId) => {
    await bookingLinksApi.removeConflictCalendar(requireAccessToken(), id, calendarId);
    set({
      bookingLinks: get().bookingLinks.map((l) =>
        l.id === id ? { ...l, conflictCalendarIds: l.conflictCalendarIds.filter((c) => c !== calendarId) } : l,
      ),
    });
  },
}));
