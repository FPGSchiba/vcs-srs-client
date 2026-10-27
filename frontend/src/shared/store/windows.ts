import { create } from "zustand";

interface WindowsState {
  /** ids of currently-open pop-out windows (mirrors the Go registry). */
  open: string[];
  setOpen: (open: string[]) => void;
}

export const useWindows = create<WindowsState>((set) => ({
  open: [],
  setOpen: (open) => set({ open }),
}));
