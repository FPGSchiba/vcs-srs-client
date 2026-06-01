import { create } from "zustand";
import type { ClientInfoDTO } from "../api/client";

interface ClientsState {
  clients: Record<string, ClientInfoDTO>;
  upsert: (guid: string, info: ClientInfoDTO) => void;
  remove: (guid: string) => void;
  replaceAll: (all: Record<string, ClientInfoDTO>) => void;
}

export const useClients = create<ClientsState>((set) => ({
  clients: {},
  upsert: (guid, info) => set((s) => ({ clients: { ...s.clients, [guid]: info } })),
  remove: (guid) => set((s) => {
    const next = { ...s.clients };
    delete next[guid];
    return { clients: next };
  }),
  replaceAll: (clients) => set({ clients }),
}));
