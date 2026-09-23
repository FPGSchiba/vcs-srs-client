import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";

import { SettingsScreen } from "./SettingsScreen";
import { useSettings } from "../../../../shared/store/settings";

// `vi.hoisted` because `vi.mock`'s factory below is hoisted above ordinary
// top-level declarations -- a plain `const audio` here would be a temporal-
// dead-zone reference by the time the factory runs.
const audio = vi.hoisted(() => ({
  input_device: "", output_device: "", input_device_name: "", output_device_name: "",
  mic_passthrough: false, agc: true, noise_suppression: true,
  vox: false, vox_threshold: 0.35, vox_min_length_ms: 220, vox_hang_ms: 300,
  vox_noise_cancel: true, ptt_start_delay_ms: 0, ptt_release_delay_ms: 120,
  voice_effect: "", clipping_effect: "",
  levels: { master: 0.75, voice: 1, sfx: 0.8, notification: 0.8 },
  effects: {},
}));

vi.mock("../../../../shared/api/client", () => ({
  api: {
    getSettings: vi.fn().mockResolvedValue({
      start_minimized: false, minimize_to_tray: true, show_transmitter_name: true,
      play_connection_sounds: true, radio_switch_as_ptt: false, audio,
    }),
    setSettings: vi.fn().mockResolvedValue(undefined),
    getKeybinds: vi.fn().mockResolvedValue([]),
    getHotkeyState: vi.fn().mockResolvedValue({ registered: true, error: "" }),
    getAudioDevices: vi.fn().mockResolvedValue({ inputs: [], outputs: [] }),
    startMicTest: vi.fn().mockResolvedValue(undefined),
    stopMicTest: vi.fn().mockResolvedValue(undefined),
    setKeybind: vi.fn(), clearKeybind: vi.fn(),
    beginCapture: vi.fn(), endCapture: vi.fn(),
  },
}));

describe("SettingsScreen", () => {
  beforeEach(() => {
    useSettings.setState({
      settings: {
        start_minimized: false, minimize_to_tray: true, show_transmitter_name: true,
        play_connection_sounds: true, radio_switch_as_ptt: false, audio,
      },
      keybinds: [],
      hotkeys: { registered: true, error: "", failed: {}, permission: "not_applicable" },
    });
  });

  it("renders all eight sections in the rail", () => {
    render(<SettingsScreen />);
    for (const label of ["General", "Keybinds", "Audio & Sounds", "Radio Effects",
                         "Profiles & Layouts", "Notifications", "Miscellaneous", "Legacy"]) {
      expect(screen.getByText(label)).toBeInTheDocument();
    }
  });

  it("opens on General with its toggles", () => {
    render(<SettingsScreen />);
    expect(screen.getByText("Start minimized")).toBeInTheDocument();
    expect(screen.getByText("Minimize to tray")).toBeInTheDocument();
  });

  it("opens Audio & Sounds on the live section, not the deferred stub", () => {
    render(<SettingsScreen />);
    fireEvent.click(screen.getByText("Audio & Sounds"));
    expect(screen.getByLabelText(/microphone/i)).toBeInTheDocument();
    expect(screen.queryByText(/Arrives in Phase/i)).not.toBeInTheDocument();
  });

  it("shows the deferred notice for a not-yet-built section", () => {
    render(<SettingsScreen />);
    fireEvent.click(screen.getByText("Radio Effects"));
    expect(screen.getByText(/Arrives in Phase 4/i)).toBeInTheDocument();
  });

  it("names the right phase per deferred section", () => {
    render(<SettingsScreen />);
    fireEvent.click(screen.getByText("Profiles & Layouts"));
    expect(screen.getByText(/Arrives in Phase 7/i)).toBeInTheDocument();
  });
});
