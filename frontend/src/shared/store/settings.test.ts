import { describe, it, expect, beforeEach } from "vitest";
import { useSettings } from "./settings";

const blank = {
  start_minimized: false, minimize_to_tray: true, show_transmitter_name: true,
  play_connection_sounds: true, radio_switch_as_ptt: false,
  audio: {
    input_device: "", output_device: "", input_device_name: "", output_device_name: "",
    mic_passthrough: false, agc: true, noise_suppression: true,
    vox: false, vox_threshold: 0.35, vox_min_length_ms: 220, vox_hang_ms: 300,
    vox_noise_cancel: true, ptt_start_delay_ms: 0, ptt_release_delay_ms: 120,
    voice_effect: "", clipping_effect: "",
    levels: { master: 0.75, voice: 1, sfx: 0.8, notification: 0.8 },
    effects: {},
  },
};

describe("settings store", () => {
  beforeEach(() => {
    useSettings.setState({ settings: null, keybinds: [], hotkeys: { registered: false, error: "", failed: {}, permission: "unknown" } });
  });

  it("starts empty", () => {
    expect(useSettings.getState().settings).toBeNull();
    expect(useSettings.getState().keybinds).toEqual([]);
  });

  it("stores settings", () => {
    useSettings.getState().setSettings(blank);
    expect(useSettings.getState().settings?.minimize_to_tray).toBe(true);
  });

  it("replaces the whole keybind list rather than merging", () => {
    const trigger = (chord: string) => [
      { kind: "key" as const, chord, device: "", device_name: "", label: chord, connected: true },
    ];
    useSettings.getState().setKeybinds([
      { action_id: "a", label: "A", desc: "", category: "global", kind: "press", triggers: trigger("F1") },
      { action_id: "b", label: "B", desc: "", category: "global", kind: "press", triggers: trigger("F2") },
    ]);
    useSettings.getState().setKeybinds([
      { action_id: "a", label: "A", desc: "", category: "global", kind: "press", triggers: trigger("F3") },
    ]);
    const kb = useSettings.getState().keybinds;
    expect(kb).toHaveLength(1);
    expect(kb[0].triggers[0].chord).toBe("F3");
  });

  it("stores hotkey registration state", () => {
    useSettings.getState().setHotkeyState({ registered: false, error: "permission denied", failed: {}, permission: "denied" });
    expect(useSettings.getState().hotkeys.error).toBe("permission denied");
  });

  it("defaults registered to true until the backend says otherwise", () => {
    // The Keybinds banner renders on `!registered`, so a pessimistic default
    // flashed "Global hotkeys unavailable" on every first paint and stuck for
    // good if getHotkeyState ever rejected. Read the untouched initial state,
    // since beforeEach above deliberately overwrites it.
    expect(useSettings.getInitialState().hotkeys.registered).toBe(true);
    expect(useSettings.getInitialState().hotkeys.error).toBe("");
  });

  it("initializes failed as empty object, not undefined", () => {
    const state = useSettings.getState();
    expect(state.hotkeys.failed).toEqual({});
    expect(state.hotkeys.failed).not.toBeUndefined();
  });

  it("preserves failed entries through setHotkeyState round-trip", () => {
    const failed = { "global.ptt": "key Numpad7 unsupported", "channel.cycle": "missing on this OS" };
    useSettings.getState().setHotkeyState({ registered: false, error: "", failed, permission: "unknown" });
    expect(useSettings.getState().hotkeys.failed).toEqual(failed);
  });
});
