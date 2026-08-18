import { create } from "zustand";

export type ThemePreference = "light" | "dark" | "system";

// Keep in sync with the pre-paint script in index.html.
const STORAGE_KEY = "calich-theme";

const darkQuery = window.matchMedia("(prefers-color-scheme: dark)");

function readStoredPreference(): ThemePreference {
  const stored = localStorage.getItem(STORAGE_KEY);
  return stored === "light" || stored === "dark" ? stored : "system";
}

function resolvesToDark(preference: ThemePreference): boolean {
  return (
    preference === "dark" || (preference === "system" && darkQuery.matches)
  );
}

function applyTheme(preference: ThemePreference): void {
  document.documentElement.classList.toggle(
    "dark",
    resolvesToDark(preference),
  );
}

interface ThemeState {
  preference: ThemePreference;
  setPreference: (preference: ThemePreference) => void;
}

export const useThemeStore = create<ThemeState>((set) => ({
  preference: readStoredPreference(),
  setPreference: (preference) => {
    localStorage.setItem(STORAGE_KEY, preference);
    applyTheme(preference);
    set({ preference });
  },
}));

// Follow OS changes live while the preference is "system".
darkQuery.addEventListener("change", () => {
  if (useThemeStore.getState().preference === "system") {
    applyTheme("system");
  }
});
