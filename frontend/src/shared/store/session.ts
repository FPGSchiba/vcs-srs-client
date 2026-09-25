import { create } from "zustand";

export type Phase = "welcome" | "connecting" | "connected";
export type Conn = "connected" | "reconnecting" | "disconnected";

/** Self is the local client's own identity, resolved at connect. */
export interface Self {
  callsign: string;
  ffid: string;
  coalition: string;
}

interface SessionState {
  phase: Phase;
  conn: Conn;
  error: string | null;
  server: string;
  self: Self | null;
  /**
   * The local client's own GUID within `ClientStateSnapshot.radios` /
   * `.clients` (empty string until a snapshot has been hydrated). Each
   * window (main, Comms popout, …) is a separate Wails webview with its own
   * JS runtime, so this is set independently wherever a window hydrates its
   * own snapshot -- see CommsApp and MainApp.
   */
  selfGuid: string;
  setPhase: (p: Phase) => void;
  setConn: (c: Conn) => void;
  setError: (e: string | null) => void;
  setServer: (s: string) => void;
  setSelf: (s: Self | null) => void;
  setSelfGuid: (guid: string) => void;
}

export const useSession = create<SessionState>((set) => ({
  phase: "welcome",
  conn: "disconnected",
  error: null,
  server: "",
  self: null,
  selfGuid: "",
  setPhase: (phase) => set({ phase }),
  setConn: (conn) => set({ conn }),
  setError: (error) => set({ error }),
  setServer: (server) => set({ server }),
  setSelf: (self) => set({ self }),
  setSelfGuid: (selfGuid) => set({ selfGuid }),
}));
