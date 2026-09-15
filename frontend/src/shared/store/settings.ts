import { create } from "zustand";

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

export interface Capture {
  code: string;
  ctrl: boolean;
  alt: boolean;
  shift: boolean;
  super: boolean;
}

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
  hotkeys: { registered: false, error: "", failed: {} },
  setSettings: (settings) => set({ settings }),
  setKeybinds: (keybinds) => set({ keybinds }),
  setHotkeyState: (hotkeys) => set({ hotkeys }),
}));
