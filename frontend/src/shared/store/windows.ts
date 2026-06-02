import { create } from "zustand";

interface WindowsState {
  /** ids of currently-open pop-out windows (mirrors the Go registry). */
  open: string[];
  setOpen: (open: string[]) => void;
  isOpen: (id: string) => boolean;
}

export const useWindows = create<WindowsState>((set, get) => ({
  open: [],
  setOpen: (open) => set({ open }),
  isOpen: (id) => get().open.includes(id),
}));
