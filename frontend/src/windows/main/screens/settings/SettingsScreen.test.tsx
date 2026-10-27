import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

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
  // Mirrors internal/audio/assets/manifest.toml's TX Start slot -- enough
  // for this file's "Radio Effects renders the live section" assertion to
  // prove a real backend-labeled row renders, without repeating the full
  // seven-slot fixture Effects.test.tsx already owns.
  effects: {
    tx_start: { enabled: false, file: "", label: "TX Start", available: false },
  },
  effect_order: ["tx_start"],
}));

vi.mock("../../../../shared/api/client", () => ({
  api: {
    getSettings: vi.fn().mockResolvedValue({
      start_minimized: false, minimize_to_tray: true, show_transmitter_name: true,
      play_connection_sounds: true, play_notification_sounds: true, radio_switch_as_ptt: false, audio,
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
        play_connection_sounds: true, play_notification_sounds: true, radio_switch_as_ptt: false, audio,
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

  it("opens Radio Effects on the live section, not the deferred stub", () => {
    render(<SettingsScreen />);
    fireEvent.click(screen.getByText("Radio Effects"));
    expect(screen.getByText("TX Start")).toBeInTheDocument();
    expect(screen.queryByText(/Arrives in Phase/i)).not.toBeInTheDocument();
  });

  it("points Profiles & Layouts at the Profiles screen", () => {
    const onNavigate = vi.fn();
    render(<SettingsScreen onNavigate={onNavigate} />);
    fireEvent.click(screen.getByText("Profiles & Layouts"));
    expect(screen.getByText("Profiles and layouts are managed on the Profiles screen.")).toBeInTheDocument();
    fireEvent.click(screen.getByText("OPEN PROFILES"));
    expect(onNavigate).toHaveBeenCalledWith("profiles");
    expect(screen.queryByText(/Arrives in Phase/i)).toBeNull();
  });

  it("Profiles button is inert without onNavigate", () => {
    render(<SettingsScreen />);
    fireEvent.click(screen.getByText("Profiles & Layouts"));
    expect(() => fireEvent.click(screen.getByText("OPEN PROFILES"))).not.toThrow();
  });

  it("Notifications is not yet scheduled and points sounds at General", () => {
    render(<SettingsScreen />);
    fireEvent.click(screen.getByText("Notifications"));
    expect(screen.getByText("Not yet scheduled")).toBeInTheDocument();
    expect(screen.getByText("Alert rules")).toBeInTheDocument();
    expect(screen.getByText("Server notifications (needs server support)")).toBeInTheDocument();
    expect(screen.getByText("Notification sounds: General → Play notification sounds.")).toBeInTheDocument();
    expect(screen.queryByText(/Sound alerts/)).toBeNull();
    expect(screen.queryByText(/Arrives in Phase/)).toBeNull();
  });

  it("Legacy is not yet scheduled", () => {
    render(<SettingsScreen />);
    fireEvent.click(screen.getByText("Legacy"));
    expect(screen.getByText("Not yet scheduled")).toBeInTheDocument();
    for (const item of ["Legacy SRS audio engine", "Legacy overlay", "Disable hardware acceleration"]) {
      expect(screen.getByText(item)).toBeInTheDocument();
    }
    expect(screen.queryByText(/Arrives in Phase/)).toBeNull();
  });

  it("only Miscellaneous names a phase", () => {
    render(<SettingsScreen />);
    for (const label of ["General", "Keybinds", "Audio & Sounds", "Radio Effects",
                         "Profiles & Layouts", "Notifications", "Legacy"]) {
      fireEvent.click(screen.getByText(label));
      expect(screen.queryByText(/Arrives in Phase/i)).toBeNull();
    }
    fireEvent.click(screen.getByText("Miscellaneous"));
    expect(screen.getByText("Arrives in Phase 10")).toBeInTheDocument();
  });

  // FE04: the section rail must work from the keyboard.
  it("every section can be tabbed to, and Enter switches to it", async () => {
    const labels = ["General", "Keybinds", "Audio & Sounds", "Radio Effects",
                    "Profiles & Layouts", "Notifications", "Miscellaneous", "Legacy"];
    render(<SettingsScreen />);
    const seen: string[] = [];
    for (let i = 0; i < labels.length; i++) {
      await userEvent.tab();
      seen.push((document.activeElement as HTMLElement).textContent ?? "");
    }
    expect(seen).toEqual(labels);
    screen.getByRole("button", { name: "Legacy" }).focus();
    await userEvent.keyboard("{Enter}");
    expect(screen.getByRole("button", { name: "Legacy" })).toHaveAttribute("aria-current", "true");
  });

  it("marks exactly one section with aria-current", () => {
    const { container } = render(<SettingsScreen />);
    const current = container.querySelectorAll("[aria-current]");
    expect(current).toHaveLength(1);
    expect(current[0]).toHaveAttribute("aria-current", "true");
    expect(current[0]).toHaveTextContent("General");
  });
});
