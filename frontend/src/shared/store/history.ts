import { create } from "zustand";
import type { HistoryEntry } from "../api/client";

/**
 * CAP mirrors internal/history.DefaultCap. The backend already evicts at
 * this bound; the renderer caps too because `history:appended` is a DELTA
 * -- a window left open across a long session would otherwise accumulate
 * rows the backend had already dropped.
 */
const CAP = 2000;

interface HistoryStore {
  entries: HistoryEntry[];
  replaceAll: (e: HistoryEntry[]) => void;
  append: (e: HistoryEntry) => void;
  clear: () => void;
}

/**
 * The log is delivered as a snapshot on mount (App.GetHistory) and as
 * single-entry deltas after that. It is NOT a full-snapshot broadcast like
 * notifications: 2000 entries is ~240KB per emit at up to several
 * transmissions per second.
 */
export const useHistory = create<HistoryStore>((set) => ({
  entries: [],
  replaceAll: (e) => set({ entries: (e ?? []).slice(0, CAP) }),
  append: (e) => set((s) => ({ entries: [e, ...s.entries].slice(0, CAP) })),
  clear: () => set({ entries: [] }),
}));
