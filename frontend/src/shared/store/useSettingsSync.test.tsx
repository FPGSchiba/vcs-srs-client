import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, waitFor } from "@testing-library/react";

const getSettings = vi.fn();
const getKeybinds = vi.fn();
const getHotkeyState = vi.fn();
const getJoystickState = vi.fn();
const getAudioDevices = vi.fn();
const getAudioState = vi.fn();
const getAudioEffectPresets = vi.fn();

vi.mock("../api/client", () => ({
  api: {
    getSettings: () => getSettings(),
    getKeybinds: () => getKeybinds(),
    getHotkeyState: () => getHotkeyState(),
    getJoystickState: () => getJoystickState(),
    getAudioDevices: () => getAudioDevices(),
    getAudioState: () => getAudioState(),
    getAudioEffectPresets: () => getAudioEffectPresets(),
  },
}));

/** Minimal stand-in for the Wails event bus: `on` registers a handler and
 * returns its unsubscribe, and `emit` drives it from the test. */
const handlers = new Map<string, Set<(d: unknown) => void>>();
const emit = (name: string, data: unknown) => handlers.get(name)?.forEach((h) => h(data));

vi.mock("../api/events", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../api/events")>();
  return {
    EV: actual.EV,
    on: (name: string, cb: (d: unknown) => void) => {
      const set = handlers.get(name) ?? new Set();
      set.add(cb);
      handlers.set(name, set);
      return () => set.delete(cb);
    },
  };
});

import { EV } from "../api/events";
import { useSettings } from "./settings";
import { useSettingsSync } from "./useSettingsSync";

const settings = {
  start_minimized: false, minimize_to_tray: true, show_transmitter_name: true,
  play_connection_sounds: true, radio_switch_as_ptt: false,
};

function Probe() {
  useSettingsSync();
  return null;
}

describe("useSettingsSync", () => {
  beforeEach(() => {
    handlers.clear();
    getSettings.mockReset().mockResolvedValue(settings);
    getKeybinds.mockReset().mockResolvedValue([]);
    getHotkeyState.mockReset().mockResolvedValue({ registered: true, error: "", failed: {}, permission: "not_applicable" });
    getJoystickState.mockReset().mockResolvedValue({ supported: false, error: "", devices: [] });
    getAudioDevices.mockReset().mockResolvedValue({ inputs: [], outputs: [] });
    getAudioState.mockReset().mockResolvedValue({
      running: false, input_error: "", output_error: "", overruns: 0, underruns: 0,
    });
    getAudioEffectPresets.mockReset().mockResolvedValue({ voice: [], clipping: [] });
    useSettings.setState({
      settings: null, keybinds: [],
      hotkeys: { registered: true, error: "", failed: {}, permission: "not_applicable" },
      joystick: { supported: false, error: "", devices: [] },
    });
  });
  afterEach(() => vi.restoreAllMocks());

  it("hydrates the store on mount", async () => {
    render(<Probe />);
    await waitFor(() => expect(useSettings.getState().settings).toEqual(settings));
  });

  it("keeps the store live via settings:changed", async () => {
    render(<Probe />);
    await waitFor(() => expect(useSettings.getState().settings).not.toBeNull());

    emit(EV.settingsChanged, { ...settings, show_transmitter_name: false });
    expect(useSettings.getState().settings?.show_transmitter_name).toBe(false);
  });

  it("keeps the store live via keybinds:changed", async () => {
    render(<Probe />);
    emit(EV.keybindsChanged, [
      {
        action_id: "global.ptt",
        label: "Global PTT",
        desc: "",
        category: "global",
        kind: "hold",
        triggers: [
          { kind: "key", chord: "F9", device: "", device_name: "", label: "F9", connected: true },
        ],
      },
    ]);
    expect(useSettings.getState().keybinds[0].triggers[0].chord).toBe("F9");
  });

  it("keeps the store live via hotkeys:state", async () => {
    render(<Probe />);
    emit(EV.hotkeysState, { registered: false, error: "permission denied", failed: {} });
    expect(useSettings.getState().hotkeys.error).toBe("permission denied");
  });

  it("unsubscribes on unmount so a remount cannot double-handle", async () => {
    const { unmount } = render(<Probe />);
    unmount();
    emit(EV.settingsChanged, { ...settings, show_transmitter_name: false });
    expect(useSettings.getState().settings).toBeNull();
  });

  it("logs rather than swallows a getHotkeyState rejection", async () => {
    // Swallowed, this left the UI claiming hotkeys were healthy with nothing
    // having confirmed it, and no trace of why.
    const err = vi.spyOn(console, "error").mockImplementation(() => {});
    getHotkeyState.mockRejectedValue(new Error("backend unreachable"));

    render(<Probe />);
    await waitFor(() => expect(err).toHaveBeenCalled());
  });

  it("hydrates joystick support on mount", async () => {
    getJoystickState.mockResolvedValue({ supported: true, error: "", devices: [{ id: "stick-c3", name: "Test Stick" }] });
    render(<Probe />);
    await waitFor(() => expect(useSettings.getState().joystick.supported).toBe(true));
    expect(useSettings.getState().joystick.devices).toEqual([{ id: "stick-c3", name: "Test Stick" }]);
  });

  it("keeps the store live via joystick:state", async () => {
    // The hydrate above runs once, on mount. Without this subscription a
    // stick plugged in afterwards stayed invisible (chips rendered muted
    // until some unrelated keybinds:changed forced a re-render), and one
    // transient enumeration error pinned "Joystick unavailable" for the life
    // of the process.
    render(<Probe />);
    await waitFor(() => expect(getJoystickState).toHaveBeenCalled());

    emit(EV.joystickState, {
      supported: true,
      error: "",
      devices: [{ id: "stick-c3", name: "Test Stick" }],
    });
    await waitFor(() => expect(useSettings.getState().joystick.supported).toBe(true));
    expect(useSettings.getState().joystick.devices).toEqual([
      { id: "stick-c3", name: "Test Stick" },
    ]);

    // And a later error state replaces it rather than accumulating.
    emit(EV.joystickState, { supported: true, error: "device read failed", devices: [] });
    await waitFor(() =>
      expect(useSettings.getState().joystick.error).toBe("device read failed"),
    );
    expect(useSettings.getState().joystick.devices).toEqual([]);
  });

  it("logs rather than swallows a getJoystickState rejection", async () => {
    // Same failure mode as getHotkeyState: the store's honest `supported:
    // false` default would otherwise hide the joystick capture affordance
    // on a machine that actually supports it, with no trace of why.
    const err = vi.spyOn(console, "error").mockImplementation(() => {});
    getJoystickState.mockRejectedValue(new Error("backend unreachable"));

    render(<Probe />);
    await waitFor(() => expect(err).toHaveBeenCalled());
  });
});
