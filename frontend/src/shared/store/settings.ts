import { create } from "zustand";
import type { Capture } from "../components/KeyChip";

/** The four mixer bus positions, normalized 0-1, mirroring Go's
 * `app.AudioLevelsDTO`. `Audio.tsx` is the one place that converts this to
 * the `Knob` component's 0-100 presentation scale -- see its doc comment. */
export interface AudioLevels {
  master: number;
  voice: number;
  sfx: number;
  notification: number;
}

/** One Radio Effects slot's persisted state, mirroring Go's
 * `app.AudioEffectDTO`. `label` and `available` are read fresh off the
 * backend's SFX manifest on every settings sync, not persisted -- see the
 * Go type's doc. `available` is false for every slot until the SFX sample
 * pack lands (internal/audio/assets/README.md); that is the honest current
 * answer, not a placeholder. */
export interface AudioEffect {
  enabled: boolean;
  file: string;
  label: string;
  available: boolean;
}

/** One selectable DSP preset (a Voice Effect or Clipping Effect dropdown
 * option), mirroring Go's `app.AudioEffectPresetDTO`. `value` is what
 * persists into `AudioSettings.voice_effect` / `.clipping_effect`. */
export interface AudioEffectPreset {
  value: string;
  label: string;
}

/** Both built-in DSP preset lists, mirroring Go's
 * `app.AudioEffectPresetsDTO`. Static data -- fetched once, not pushed on
 * an event, unlike `audioDevices`/`audioState`. */
export interface AudioEffectPresets {
  voice: AudioEffectPreset[];
  clipping: AudioEffectPreset[];
}

/** Persisted audio configuration, mirroring Go's `app.AudioSettingsDTO`
 * field-for-field. `levels` and `vox_threshold` are normalized 0-1 here --
 * see `AudioLevels`'s doc for where the presentation-scale conversion
 * lives. */
export interface AudioSettings {
  input_device: string;
  output_device: string;
  input_device_name: string;
  output_device_name: string;
  mic_passthrough: boolean;
  agc: boolean;
  noise_suppression: boolean;
  vox: boolean;
  vox_threshold: number;
  vox_min_length_ms: number;
  vox_hang_ms: number;
  vox_noise_cancel: boolean;
  ptt_start_delay_ms: number;
  ptt_release_delay_ms: number;
  voice_effect: string;
  clipping_effect: string;
  levels: AudioLevels;
  effects: Record<string, AudioEffect>;
  /** `effects`' keys, in the manifest's display order. A JSON object's key
   * order is not guaranteed (and Go's encoding/json sorts map keys
   * alphabetically), so a stable render order travels as its own array --
   * see Go's `AudioSettingsDTO.EffectOrder` doc. Derived, like `label`/
   * `available`: never written back through `setSettings`. */
  effect_order: string[];
}

export interface Settings {
  start_minimized: boolean;
  minimize_to_tray: boolean;
  show_transmitter_name: boolean;
  play_connection_sounds: boolean;
  radio_switch_as_ptt: boolean;
  audio: AudioSettings;
}

/** One selectable audio endpoint, mirroring Go's `app.AudioDeviceDTO`. */
export interface AudioDevice {
  id: string;
  name: string;
  is_default: boolean;
}

/** The full enumerated device list for both directions, mirroring Go's
 * `app.AudioDevicesDTO`. */
export interface AudioDevices {
  inputs: AudioDevice[];
  outputs: AudioDevice[];
}

/** Audio subsystem health, mirroring Go's `app.AudioStateDTO`. */
export interface AudioState {
  running: boolean;
  starting: boolean;
  input_error: string;
  output_error: string;
  overruns: number;
  underruns: number;
  /** The device ids actually in use, which is not necessarily what the
   * settings asked for -- see the `_substituted` pair. */
  input_device: string;
  output_device: string;
  /** The id above differs from the configured one because the saved device
   * no longer enumerates and the engine fell back to the OS default. */
  input_substituted: boolean;
  output_substituted: boolean;
}

/** The ~20Hz meter reading, mirroring Go's `audio.VU` as carried on the
 * `audio:vu` event. Normalized 0-1 per bus, like `VU`'s `level` prop. */
export interface AudioVU {
  input: number;
  output: number;
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

/** The action that lost a trigger to a new binding, mirroring Go's
 * `app.StolenDTO`. It reaches the UI two ways: as `SetKeybindResult.stolen`
 * for a keyboard capture (which round-trips through `addTrigger`), and on the
 * `keybinds:joy_captured` event for a joystick capture (which completes in the
 * backend and returns to no caller). Both feed the same banner. */
export interface Stolen {
  action_id: string;
  label: string;
  trigger: Trigger;
}

export interface SetKeybindResult {
  stolen: Stolen | null;
}

interface SettingsState {
  settings: Settings | null;
  keybinds: Keybind[];
  hotkeys: HotkeyState;
  joystick: JoystickState;
  audioDevices: AudioDevices;
  audioState: AudioState;
  audioEffectPresets: AudioEffectPresets;
  vu: AudioVU;
  micMuted: boolean;
  setSettings: (s: Settings) => void;
  setKeybinds: (k: Keybind[]) => void;
  setHotkeyState: (h: HotkeyState) => void;
  setJoystickState: (j: JoystickState) => void;
  setAudioDevices: (d: AudioDevices) => void;
  setAudioState: (a: AudioState) => void;
  setAudioEffectPresets: (p: AudioEffectPresets) => void;
  setVU: (v: AudioVU) => void;
  setMicMuted: (m: boolean) => void;
}

export const useSettings = create<SettingsState>((set) => ({
  settings: null,
  keybinds: [],
  audioDevices: { inputs: [], outputs: [] },
  // Honest, not optimistic, matching the reasoning already written into the
  // `joystick` default above: whether the audio subsystem is actually
  // running is unknown until `GetAudioState()` resolves (SetAudioBackend may
  // never have been called -- see audio.go's doc), so `running: false` and
  // empty errors is the state that claims nothing the backend hasn't
  // confirmed, rather than optimistically claiming health up front the way
  // `hotkeys.registered` does for a subsystem that mostly works.
  audioState: {
    running: false,
    starting: false,
    input_error: "",
    output_error: "",
    overruns: 0,
    underruns: 0,
    input_device: "",
    output_device: "",
    input_substituted: false,
    output_substituted: false,
  },
  // Empty until getAudioEffectPresets() resolves. Effects.tsx renders no
  // preset options in that brief window rather than a stale/guessed list.
  audioEffectPresets: { voice: [], clipping: [] },
  vu: { input: 0, output: 0 },
  micMuted: false,
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
  setAudioDevices: (audioDevices) => set({ audioDevices }),
  setAudioState: (audioState) => set({ audioState }),
  setAudioEffectPresets: (audioEffectPresets) => set({ audioEffectPresets }),
  setVU: (vu) => set({ vu }),
  setMicMuted: (micMuted) => set({ micMuted }),
}));
