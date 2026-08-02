import { create } from "zustand";
import { mockAuthApi, type Session } from "./mockAuthApi";

const STORAGE_KEY = "calendar.session";

function isSessionShaped(value: unknown): value is Session {
  if (typeof value !== "object" || value === null) return false;
  const candidate = value as Record<string, unknown>;
  return (
    typeof candidate.accessToken === "string" &&
    typeof candidate.refreshToken === "string" &&
    typeof candidate.user === "object" &&
    candidate.user !== null &&
    typeof (candidate.user as Record<string, unknown>).email === "string"
  );
}

function loadStoredSession(): Session | null {
  const raw = localStorage.getItem(STORAGE_KEY);
  if (!raw) return null;
  try {
    const parsed: unknown = JSON.parse(raw);
    return isSessionShaped(parsed) ? parsed : null;
  } catch {
    return null;
  }
}

function persistSession(session: Session | null) {
  if (session) {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(session));
  } else {
    localStorage.removeItem(STORAGE_KEY);
  }
}

interface AuthState {
  session: Session | null;
  login: (email: string, password: string) => Promise<void>;
  logout: () => void;
  refreshAccessToken: () => Promise<void>;
}

export const useAuthStore = create<AuthState>((set, get) => ({
  session: loadStoredSession(),
  login: async (email, password) => {
    const session = await mockAuthApi.login(email, password);
    persistSession(session);
    set({ session });
  },
  logout: () => {
    persistSession(null);
    set({ session: null });
  },
  refreshAccessToken: async () => {
    const { session } = get();
    if (!session) return;
    const { accessToken } = await mockAuthApi.refresh(session.refreshToken);
    const nextSession = { ...session, accessToken };
    persistSession(nextSession);
    set({ session: nextSession });
  },
}));
