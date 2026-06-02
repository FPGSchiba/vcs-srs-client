import { create } from "zustand";
import type { RadioInfoDTO } from "../api/client";

interface RadiosState {
  radios: Record<string, RadioInfoDTO>;
  setForGuid: (guid: string, info: RadioInfoDTO) => void;
  replaceAll: (all: Record<string, RadioInfoDTO>) => void;
}

export const useRadios = create<RadiosState>((set) => ({
  radios: {},
  setForGuid: (guid, info) => set((s) => ({ radios: { ...s.radios, [guid]: info } })),
  replaceAll: (radios) => set({ radios }),
}));
