import { create } from "zustand";
import type { Capture } from "../components/KeyChip";

export interface Settings {
  start_minimized: boolean;
  minimize_to_tray: boolean;
  show_transmitter_name: boolean;
  play_connection_sounds: boolean;
  radio_switch_as_ptt: boolean;
}

export interface Keybind {
  action_id: string;
  label: string;
  desc: string;
  category: string;
  kind: string;
  chord: string;
}

export interface HotkeyState {
  registered: boolean;
  error: string;
  failed: Record<string, string>;
}

export type { Capture };

export interface SetKeybindResult {
  stolen: { action_id: string; label: string; chord: string } | null;
}

interface SettingsState {
  settings: Settings | null;
  keybinds: Keybind[];
  hotkeys: HotkeyState;
  setSettings: (s: Settings) => void;
  setKeybinds: (k: Keybind[]) => void;
  setHotkeyState: (h: HotkeyState) => void;
}

export const useSettings = create<SettingsState>((set) => ({
  settings: null,
  keybinds: [],
  // `registered: true` is the optimistic default on purpose. The Keybinds
  // section renders "Global hotkeys unavailable" whenever this is false, so
  // defaulting to false flashed that banner on every first paint, before
  // getHotkeyState() had resolved -- and left it up permanently if that call
  // ever rejected. Nothing is known to be broken until the backend says so.
  hotkeys: { registered: true, error: "", failed: {} },
  setSettings: (settings) => set({ settings }),
  setKeybinds: (keybinds) => set({ keybinds }),
  setHotkeyState: (hotkeys) => set({ hotkeys }),
}));
