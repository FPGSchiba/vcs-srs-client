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
  self: Self | null;
  /**
   * The local client's own GUID within `ClientStateSnapshot.radios` /
   * `.clients` (empty string until a snapshot has been hydrated). Each
   * window (main, Comms popout, …) is a separate Wails webview with its own
   * JS runtime, so this is set independently wherever a window hydrates its
   * own snapshot -- see CommsApp and MainApp.
   */
  selfGuid: string;
  /**
   * Why the server ended the last session (Phase 6.9 CL1), shown by the
   * Welcome screen's alert slot; null when there is nothing to say. Set by
   * useSessionEndedNotice, cleared by the next CONNECT or BACK.
   */
  sessionNotice: string | null;
  setPhase: (p: Phase) => void;
  setConn: (c: Conn) => void;
  setSelf: (s: Self | null) => void;
  setSelfGuid: (guid: string) => void;
  setSessionNotice: (n: string | null) => void;
}

export const useSession = create<SessionState>((set) => ({
  phase: "welcome",
  conn: "disconnected",
  self: null,
  selfGuid: "",
  sessionNotice: null,
  setPhase: (phase) => set({ phase }),
  setConn: (conn) => set({ conn }),
  setSelf: (self) => set({ self }),
  setSelfGuid: (selfGuid) => set({ selfGuid }),
  setSessionNotice: (sessionNotice) => set({ sessionNotice }),
}));
