import { create } from "zustand";
import { useCalendarsStore } from "./calendarsStore";

export type ActiveView = "day" | "week" | "month" | "year";

interface ShellState {
  selectedDate: Date;
  activeView: ActiveView;
  checkedCalendarIds: Set<string>;
  setSelectedDate: (date: Date) => void;
  setActiveView: (view: ActiveView) => void;
  setCheckedCalendarIds: (ids: Iterable<string>) => void;
  toggleCalendarChecked: (id: string) => void;
  removeCheckedCalendarId: (id: string) => void;
}

export const useShellStore = create<ShellState>((set) => ({
  selectedDate: new Date(),
  activeView: "month",
  checkedCalendarIds: new Set(
    useCalendarsStore.getState().calendars.map((calendar) => calendar.id),
  ),
  setSelectedDate: (date) => set({ selectedDate: date }),
  setActiveView: (view) => set({ activeView: view }),
  setCheckedCalendarIds: (ids) => set({ checkedCalendarIds: new Set(ids) }),
  toggleCalendarChecked: (id) =>
    set((state) => {
      const next = new Set(state.checkedCalendarIds);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return { checkedCalendarIds: next };
    }),
  removeCheckedCalendarId: (id) =>
    set((state) => {
      const next = new Set(state.checkedCalendarIds);
      next.delete(id);
      return { checkedCalendarIds: next };
    }),
}));
