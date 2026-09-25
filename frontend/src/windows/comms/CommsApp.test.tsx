import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";

const getClientState = vi.fn();
const getSettings = vi.fn();
const getKeybinds = vi.fn();
const getHotkeyState = vi.fn();
const getJoystickState = vi.fn();
const getAudioDevices = vi.fn();
const getAudioState = vi.fn();
const getAudioEffectPresets = vi.fn();
const voiceState = vi.fn();

vi.mock("../../shared/api/client", () => ({
  api: {
    getClientState: () => getClientState(),
    getSettings: () => getSettings(),
    getKeybinds: () => getKeybinds(),
    getHotkeyState: () => getHotkeyState(),
    getJoystickState: () => getJoystickState(),
    getAudioDevices: () => getAudioDevices(),
    getAudioState: () => getAudioState(),
    getAudioEffectPresets: () => getAudioEffectPresets(),
    voiceState: () => voiceState(),
    closeWindow: vi.fn(),
    updateRadioInfo: vi.fn(),
    selectRadio: vi.fn(),
  },
}));

const handlers = new Map<string, Set<(d: unknown) => void>>();
const emit = (name: string, data: unknown) => handlers.get(name)?.forEach((h) => h(data));

vi.mock("../../shared/api/events", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../../shared/api/events")>();
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

import { EV } from "../../shared/api/events";
import { useSettings } from "../../shared/store/settings";
import { useRadios } from "../../shared/store/radios";
import { useSession } from "../../shared/store/session";
import { CommsApp } from "./CommsApp";

const settings = {
  start_minimized: false, minimize_to_tray: true, show_transmitter_name: true,
  play_connection_sounds: true, radio_switch_as_ptt: false,
};

/**
 * Spec DoD 10: "Settings and keybind changes reach the Comms popout without
 * reopening it." The subscriptions used to live in SettingsScreen, a
 * main-window-only component, so this window never observed them at all.
 */
describe("CommsApp settings sync", () => {
  beforeEach(() => {
    handlers.clear();
    getClientState.mockReset().mockResolvedValue({ radios: {}, clients: {}, self: null, self_guid: "" });
    getSettings.mockReset().mockResolvedValue(settings);
    getKeybinds.mockReset().mockResolvedValue([]);
    getHotkeyState.mockReset().mockResolvedValue({ registered: true, error: "", failed: {}, permission: "not_applicable" });
    getJoystickState.mockReset().mockResolvedValue({ supported: false, error: "", devices: [] });
    getAudioDevices.mockReset().mockResolvedValue({ inputs: [], outputs: [] });
    getAudioState.mockReset().mockResolvedValue({
      running: false, input_error: "", output_error: "", overruns: 0, underruns: 0,
    });
    getAudioEffectPresets.mockReset().mockResolvedValue({ voice: [], clipping: [] });
    voiceState.mockReset().mockResolvedValue({ selected_radio: 0, connected: false });
    useSettings.setState({
      settings: null, keybinds: [],
      hotkeys: { registered: true, error: "", failed: {}, permission: "not_applicable" },
      joystick: { supported: false, error: "", devices: [] },
    });
    useRadios.setState({ radios: {}, selectedRadioId: 0, heldPTT: new Set() });
    useSession.setState({ selfGuid: "" });
  });

  it("hydrates the shared settings store when the popout opens", async () => {
    render(<CommsApp />);
    await waitFor(() => expect(useSettings.getState().settings).toEqual(settings));
  });

  it("applies a settings change made in the main window, without reopening", async () => {
    render(<CommsApp />);
    await waitFor(() => expect(useSettings.getState().settings).not.toBeNull());

    emit(EV.settingsChanged, { ...settings, show_transmitter_name: false });
    expect(useSettings.getState().settings?.show_transmitter_name).toBe(false);
  });

  it("applies a keybind change made in the main window, without reopening", async () => {
    render(<CommsApp />);
    emit(EV.keybindsChanged, [
      {
        action_id: "radio.1.ptt",
        label: "R01 (PTT)",
        desc: "",
        category: "per_radio",
        kind: "hold",
        triggers: [
          { kind: "key", chord: "F7", device: "", device_name: "", label: "F7", connected: true },
        ],
      },
    ]);
    expect(useSettings.getState().keybinds[0].triggers[0].chord).toBe("F7");
  });
});

/**
 * CommsApp used to render `Object.values(radios)[0]` -- the first entry of
 * the WHOLE radios map, which is any client's radios, not necessarily ours.
 * Harmless while nobody else was connected; wrong the moment voice makes
 * multi-client sessions real. It must render `radios[selfGuid]` instead.
 */
describe("CommsApp radio selection", () => {
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
    voiceState.mockReset().mockResolvedValue({ selected_radio: 0, connected: true });
    useSettings.setState({
      settings: null, keybinds: [],
      hotkeys: { registered: true, error: "", failed: {}, permission: "not_applicable" },
      joystick: { supported: false, error: "", devices: [] },
    });
    useRadios.setState({ radios: {}, selectedRadioId: 0, heldPTT: new Set() });
    useSession.setState({ selfGuid: "" });
  });

  it("renders the local client's radios, keyed by self_guid, not the first map entry", async () => {
    getClientState.mockReset().mockResolvedValue({
      self_guid: "guid-me",
      self: { name: "Me", coalition: "Red", unit_id: "AB12", role_id: 0 },
      clients: {
        "guid-other": { name: "Other", coalition: "Red", unit_id: "CD34", role_id: 0 },
        "guid-me": { name: "Me", coalition: "Red", unit_id: "AB12", role_id: 0 },
      },
      // "guid-other" sorts first in insertion order -- Object.values(...)[0]
      // would pick its radios, not ours.
      radios: {
        "guid-other": {
          muted: false,
          radios: [{ id: 9, name: "Not Mine", frequency: 251.0, enabled: true, is_intercom: false }],
        },
        "guid-me": {
          muted: false,
          radios: [{ id: 1, name: "Mine", frequency: 118.5, enabled: true, is_intercom: false }],
        },
      },
    });

    render(<CommsApp />);

    expect(await screen.findByDisplayValue("Mine")).toBeInTheDocument();
    expect(screen.queryByDisplayValue("Not Mine")).not.toBeInTheDocument();
  });

  it("shows the empty state when the local client has no radios yet", async () => {
    getClientState.mockReset().mockResolvedValue({
      self_guid: "guid-me",
      self: { name: "Me", coalition: "Red", unit_id: "AB12", role_id: 0 },
      clients: { "guid-me": { name: "Me", coalition: "Red", unit_id: "AB12", role_id: 0 } },
      radios: {},
    });

    render(<CommsApp />);

    expect(await screen.findByText(/no radios/i)).toBeInTheDocument();
  });
});
