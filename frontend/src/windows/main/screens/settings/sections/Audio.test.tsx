import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { Audio } from "./Audio";
import { useSettings } from "../../../../../shared/store/settings";

const setSettings = vi.fn();
vi.mock("../../../../../shared/api/client", () => ({
  api: {
    setSettings: (...a: unknown[]) => setSettings(...a),
    getAudioDevices: () => Promise.resolve({ inputs: [], outputs: [] }),
    startMicTest: () => Promise.resolve(),
    stopMicTest: () => Promise.resolve(),
  },
}));

function seed() {
  useSettings.setState({
    settings: {
      start_minimized: false, minimize_to_tray: true, show_transmitter_name: true,
      play_connection_sounds: true, radio_switch_as_ptt: false,
      audio: {
        input_device: "", output_device: "", input_device_name: "", output_device_name: "",
        mic_passthrough: false, agc: true, noise_suppression: true,
        vox: false, vox_threshold: 0.35, vox_min_length_ms: 220, vox_hang_ms: 300,
        vox_noise_cancel: true, ptt_start_delay_ms: 0, ptt_release_delay_ms: 120,
        voice_effect: "comms_filter_mid", clipping_effect: "",
        levels: { master: 0.75, voice: 1, sfx: 0.8, notification: 0.8 },
        effects: {},
      },
    },
    audioDevices: {
      inputs: [{ id: "", name: "System Default", is_default: true }, { id: "mic-1", name: "Procyon Headset", is_default: false }],
      outputs: [{ id: "", name: "System Default", is_default: true }],
    },
  });
}

describe("Audio settings", () => {
  beforeEach(() => { setSettings.mockClear(); seed(); });

  it("lists enumerated input devices including System Default", () => {
    render(<Audio />);
    expect(screen.getByRole("option", { name: "System Default" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "Procyon Headset" })).toBeInTheDocument();
  });

  it("persists a device change through setSettings", async () => {
    render(<Audio />);
    await userEvent.selectOptions(screen.getByLabelText(/microphone/i), "mic-1");
    await waitFor(() => expect(setSettings).toHaveBeenCalled());
    expect(setSettings.mock.calls[0][0].audio.input_device).toBe("mic-1");
  });

  it("hides the VOX detail rows until VOX is enabled", async () => {
    render(<Audio />);
    expect(screen.queryByLabelText(/vox threshold/i)).not.toBeInTheDocument();
    await userEvent.click(screen.getByLabelText(/voice activation/i));
    await waitFor(() => expect(setSettings).toHaveBeenCalled());
    expect(setSettings.mock.calls[0][0].audio.vox).toBe(true);
  });

  it("renders all four level knobs", () => {
    render(<Audio />);
    for (const name of [/master/i, /voice/i, /sfx/i, /notification/i]) {
      expect(screen.getByRole("slider", { name })).toBeInTheDocument();
    }
  });

  it("renders the live VU meter from store state", () => {
    useSettings.setState({ vu: { input: 0.5, output: 0 } });
    const { container } = render(<Audio />);
    expect(container.querySelectorAll("[data-vu-seg]").length).toBeGreaterThan(0);
  });
});
