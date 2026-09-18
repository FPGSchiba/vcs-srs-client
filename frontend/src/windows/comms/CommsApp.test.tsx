import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, waitFor } from "@testing-library/react";

const getClientState = vi.fn();
const getSettings = vi.fn();
const getKeybinds = vi.fn();
const getHotkeyState = vi.fn();

vi.mock("../../shared/api/client", () => ({
  api: {
    getClientState: () => getClientState(),
    getSettings: () => getSettings(),
    getKeybinds: () => getKeybinds(),
    getHotkeyState: () => getHotkeyState(),
    closeWindow: vi.fn(),
    updateRadioInfo: vi.fn(),
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
    useSettings.setState({
      settings: null, keybinds: [], hotkeys: { registered: true, error: "", failed: {}, permission: "not_applicable" },
    });
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
      { action_id: "radio.1.ptt", label: "R01 (PTT)", desc: "", category: "per_radio", kind: "hold", chord: "F7" },
    ]);
    expect(useSettings.getState().keybinds[0].chord).toBe("F7");
  });
});
