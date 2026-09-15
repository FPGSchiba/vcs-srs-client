import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";

import { SettingsScreen } from "./SettingsScreen";
import { useSettings } from "../../../../shared/store/settings";

vi.mock("../../../../shared/api/client", () => ({
  api: {
    getSettings: vi.fn().mockResolvedValue({
      start_minimized: false, minimize_to_tray: true, show_transmitter_name: true,
      play_connection_sounds: true, radio_switch_as_ptt: false,
    }),
    setSettings: vi.fn().mockResolvedValue(undefined),
    getKeybinds: vi.fn().mockResolvedValue([]),
    getHotkeyState: vi.fn().mockResolvedValue({ registered: true, error: "" }),
    setKeybind: vi.fn(), clearKeybind: vi.fn(),
    beginCapture: vi.fn(), endCapture: vi.fn(),
  },
}));

describe("SettingsScreen", () => {
  beforeEach(() => {
    useSettings.setState({
      settings: {
        start_minimized: false, minimize_to_tray: true, show_transmitter_name: true,
        play_connection_sounds: true, radio_switch_as_ptt: false,
      },
      keybinds: [],
      hotkeys: { registered: true, error: "", failed: {} },
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

  it("shows the deferred notice for a not-yet-built section", () => {
    render(<SettingsScreen />);
    fireEvent.click(screen.getByText("Audio & Sounds"));
    expect(screen.getByText(/Arrives in Phase 4/i)).toBeInTheDocument();
  });

  it("names the right phase per deferred section", () => {
    render(<SettingsScreen />);
    fireEvent.click(screen.getByText("Profiles & Layouts"));
    expect(screen.getByText(/Arrives in Phase 7/i)).toBeInTheDocument();
  });
});
