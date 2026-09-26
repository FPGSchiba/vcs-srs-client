import { create } from "zustand";

/** Status-dot colour classes, matching `.dual-pill .d.*` in the design CSS.
 *  `off` is the dimmed, non-alarming rendering for an unavailable plane. */
export type Dot = "ok" | "warn" | "alert" | "off";

/** Which ConnBanner variant to render, matching the design prototype's
 *  `shell.jsx` ConnBanner states. */
export type BannerVariant = "none" | "control-only" | "voice-only" | "disconnected";

/** One transport plane's health. Mirrors Go's `app.ConnLinkDTO`. */
export interface ConnLink {
  state: string;
  /** -1 means nothing has been measured. NOT 0 — see formatRTT. */
  rtt_ms: number;
  healthy: boolean;
  available: boolean;
  error: string;
}

/** The full dual-plane snapshot. Mirrors Go's `app.ConnectionStateDTO`. */
export interface ConnectionState {
  server: string;
  control: ConnLink;
  voice: ConnLink;
}

/** The honest pre-hydration state: disconnected, nothing measured, voice
 *  unavailable. Deliberately not optimistic — claiming health nothing has
 *  confirmed is the failure mode `useSettingsSync`'s getHotkeyState comment
 *  already calls out. */
export function emptyConnection(): ConnectionState {
  return {
    server: "",
    control: { state: "disconnected", rtt_ms: -1, healthy: false, available: true, error: "" },
    voice: { state: "unavailable", rtt_ms: -1, healthy: false, available: false, error: "" },
  };
}

/** Voice lifecycle states that mean "working on it", from `voice.State`. */
const VOICE_TRANSIENT = ["resolving", "handshaking", "rebinding", "retrying"];

export function controlDot(l: ConnLink): Dot {
  if (l.state === "disconnected") return "alert";
  if (l.state === "reconnecting") return "warn";
  // Connected but the probe has begun failing. The dot moves before the
  // banner does, so the user gets build-up rather than a banner appearing
  // out of nowhere 15s into an outage.
  if (!l.healthy) return "warn";
  return "ok";
}

export function voiceDot(l: ConnLink): Dot {
  // Checked first and unconditionally: an unavailable plane has no
  // lifecycle worth colouring, and rendering it as an alert would put a red
  // dot on every CGO-less Windows release build as its normal state.
  if (!l.available) return "off";
  if (l.state === "connected") return "ok";
  if (VOICE_TRANSIENT.includes(l.state)) return "warn";
  return "alert";
}

export function bannerVariant(c: ConnectionState): BannerVariant {
  const controlDown = controlDot(c.control) === "alert";
  // An unavailable voice plane drops out of the banner's input entirely --
  // EXCEPT when control is already down. In that case there is no live
  // plane left to call "voice-only" about, so it must read as a full
  // disconnect rather than implying voice is somehow still up.
  const voiceAvailable = c.voice.available;
  const voiceDown = voiceAvailable && voiceDot(c.voice) === "alert";

  if (controlDown && (voiceDown || !voiceAvailable)) return "disconnected";
  if (controlDown) return "voice-only";
  if (voiceDown) return "control-only";
  return "none";
}

/** Renders a latency figure, or an em dash when nothing has been measured.
 *  The -1 sentinel exists because 0 is a value the voice plane genuinely
 *  reports — before its first answered keepalive, and after every rebind. */
export function formatRTT(ms: number): string {
  if (ms < 0) return "—";
  return `${ms}ms`;
}

interface ConnectionStore {
  conn: ConnectionState;
  setConn: (c: ConnectionState) => void;
}

export const useConnection = create<ConnectionStore>((set) => ({
  conn: emptyConnection(),
  setConn: (conn) => set({ conn }),
}));
