import { create } from "zustand";

export type Phase = "welcome" | "connecting" | "connected";
export type Conn = "connected" | "reconnecting" | "disconnected";

interface SessionState {
  phase: Phase;
  conn: Conn;
  error: string | null;
  server: string;
  setPhase: (p: Phase) => void;
  setConn: (c: Conn) => void;
  setError: (e: string | null) => void;
  setServer: (s: string) => void;
}

export const useSession = create<SessionState>((set) => ({
  phase: "welcome",
  conn: "disconnected",
  error: null,
  server: "",
  setPhase: (phase) => set({ phase }),
  setConn: (conn) => set({ conn }),
  setError: (error) => set({ error }),
  setServer: (server) => set({ server }),
}));
