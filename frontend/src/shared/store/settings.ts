import { create } from "zustand";
import type { Capture } from "../components/KeyChip";

export interface Settings {
  start_minimized: boolean;
  minimize_to_tray: boolean;
  show_transmitter_name: boolean;
  play_connection_sounds: boolean;
  radio_switch_as_ptt: boolean;
}

/** One way to activate an action. Mirrors Go's `app.TriggerDTO`.
 *
 * `label` is rendered by the backend, not here: the physical naming of
 * buttons and hats has exactly one home, the same way canonical chord
 * formatting lives in Go's `internal/chord`. */
export interface Trigger {
  kind: "key" | "joy";
  chord: string;
  device: string;
  device_name: string;
  label: string;
  connected: boolean;
}

export interface Keybind {
  action_id: string;
  label: string;
  desc: string;
  category: string;
  kind: string;
  triggers: Trigger[];
}

export interface JoystickDevice {
  id: string;
  name: string;
}

/** Joystick subsystem health, mirroring Go's `app.JoystickStateDTO`.
 *
 * `supported: false` (macOS) means HIDE the affordance -- it is explicitly
 * NOT a permission denial, so it must never render a grant button. There is
 * nothing the user can do about it. */
export interface JoystickState {
  supported: boolean;
  error: string;
  devices: JoystickDevice[];
}

/** OS grant state for global hotkey capture, mirroring Go's
 * `hotkeys.Permission.String()`. Only macOS can report "denied"; Windows and
 * Linux/X11 report "not_applicable", which the UI reads as "offer no
 * permission affordance at all". */
export type HotkeyPermission = "unknown" | "granted" | "denied" | "not_applicable";

export interface HotkeyState {
  registered: boolean;
  error: string;
  failed: Record<string, string>;
  permission: HotkeyPermission;
}

/** Result of `api.requestHotkeyPermission()`. `prompted` is what the OS
 * request call returned and is NOT the user's answer -- macOS answers the
 * prompt asynchronously through TCC. Its only use is choosing the banner's
 * next button: `prompted: false` while `permission` is still "denied" is
 * evidence that System Settings may be the remaining route (it covers both
 * "the sheet is up, unanswered" and "already refused"), which is why the UI
 * offers that route alongside a re-check rather than instead of one. */
export interface HotkeyPermissionResult {
  prompted: boolean;
  permission: HotkeyPermission;
}

export type { Capture };

export interface SetKeybindResult {
  stolen: { action_id: string; label: string; trigger: Trigger } | null;
}

interface SettingsState {
  settings: Settings | null;
  keybinds: Keybind[];
  hotkeys: HotkeyState;
  joystick: JoystickState;
  setSettings: (s: Settings) => void;
  setKeybinds: (k: Keybind[]) => void;
  setHotkeyState: (h: HotkeyState) => void;
  setJoystickState: (j: JoystickState) => void;
}

export const useSettings = create<SettingsState>((set) => ({
  settings: null,
  keybinds: [],
  // `registered: true` is the optimistic default on purpose. The Keybinds
  // section renders "Global hotkeys unavailable" whenever this is false, so
  // defaulting to false flashed that banner on every first paint, before
  // getHotkeyState() had resolved -- and left it up permanently if that call
  // ever rejected. Nothing is known to be broken until the backend says so.
  // `permission: "unknown"` rather than an optimistic guess: the banner's
  // permission copy only renders on the exact string "denied", so an
  // unknown state shows nothing, and nothing claims a grant the backend has
  // not reported.
  hotkeys: { registered: true, error: "", failed: {}, permission: "unknown" },
  // `supported: false` is the honest default, not an optimistic guess: unlike
  // hotkeys (which mostly work), a real joystick subsystem is the exception
  // until `getJoystickState()` proves otherwise, and `supported: false` is
  // exactly the state that hides the affordance -- the safe default.
  joystick: { supported: false, error: "", devices: [] },
  setSettings: (settings) => set({ settings }),
  setKeybinds: (keybinds) => set({ keybinds }),
  setHotkeyState: (hotkeys) => set({ hotkeys }),
  setJoystickState: (joystick) => set({ joystick }),
}));
