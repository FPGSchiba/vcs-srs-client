import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Audio } from "./Audio";
import { useSettings } from "../../../../../shared/store/settings";
import { usePendingSettings } from "../../../../../shared/store/settingsCommit";

const setSettings = vi.fn();
const startMicTest = vi.fn(() => Promise.resolve());
vi.mock("../../../../../shared/api/client", () => ({
  api: {
    setSettings: (...a: unknown[]) => setSettings(...a),
    getAudioDevices: () => Promise.resolve({ inputs: [], outputs: [] }),
    startMicTest: () => startMicTest(),
    stopMicTest: () => Promise.resolve(),
  },
}));

function seed() {
  useSettings.setState({
    settings: {
      start_minimized: false, minimize_to_tray: true, show_transmitter_name: true,
      play_connection_sounds: true, play_notification_sounds: true, radio_switch_as_ptt: false,
      audio: {
        input_device: "", output_device: "", input_device_name: "", output_device_name: "",
        mic_passthrough: false, agc: true, noise_suppression: true,
        vox: false, vox_threshold: 0.35, vox_min_length_ms: 220, vox_hang_ms: 300,
        vox_noise_cancel: true, ptt_start_delay_ms: 0, ptt_release_delay_ms: 120,
        voice_effect: "comms_filter_mid", clipping_effect: "",
        levels: { master: 0.75, voice: 1, sfx: 0.8, notification: 0.8 },
        effects: {},
        effect_order: [],
      },
    },
    // Real enumerated endpoints ONLY. The { id: "", name: "System Default" }
    // sentinel is synthesised in Go (internal/app.AudioDeviceDTOs) and
    // arrives here already prepended in production; putting it in this
    // fixture and then asserting it rendered -- which is what this file used
    // to do -- proved nothing except that the fixture contained what the
    // fixture contained. The assertion that the sentinel EXISTS lives where
    // it can actually fail: TestGetAudioDevicesLeadsWithSystemDefault in
    // internal/app/audio_test.go.
    audioDevices: {
      inputs: [{ id: "mic-1", name: "Procyon Headset", is_default: true }],
      outputs: [{ id: "out-1", name: "Bridge Speakers", is_default: true }],
    },
  });
}

describe("Audio settings", () => {
  beforeEach(() => {
    setSettings.mockClear();
    startMicTest.mockReset().mockImplementation(() => Promise.resolve());
    usePendingSettings.setState({ pending: null });
    seed();
  });

  it("renders exactly the devices the backend hands it, synthesising none of its own", () => {
    render(<Audio />);
    expect(screen.getByRole("option", { name: "Procyon Headset" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "Bridge Speakers" })).toBeInTheDocument();
    // The fixture carries no "System Default" entry, so one appearing here
    // could only have been invented by this component -- which is exactly
    // the duplication the Go-side prepend exists to avoid.
    expect(screen.queryByRole("option", { name: "System Default" })).not.toBeInTheDocument();
    expect(screen.queryByRole("option", { name: "System Default (output)" })).not.toBeInTheDocument();
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
    const { container, unmount } = render(<Audio />);
    // Asserting segment count alone would pass even if the meter's `level`
    // prop were hardcoded to 0 -- VU always renders 16 <span data-vu-seg>
    // elements regardless of level (see VU.tsx). The lit COUNT is what
    // actually proves the store's value reached the meter: VU.tsx lights
    // segment i when level >= (i+1)/segs, so level=0.5 over 16 segments
    // lights exactly floor(0.5*16) = 8.
    const lit = () => container.querySelectorAll('[data-vu-seg][data-lit="true"]');
    expect(lit().length).toBe(8);
    unmount();

    // A second, different level rules out a coincidental match: if lit
    // count tracked nothing (e.g. a hardcoded level), both renders would
    // report the same count regardless of store state.
    useSettings.setState({ vu: { input: 0.9375, output: 0 } }); // 15/16
    const { container: container2 } = render(<Audio />);
    const lit2 = container2.querySelectorAll('[data-vu-seg][data-lit="true"]');
    expect(lit2.length).toBe(15);
  });

  it("a rejected startMicTest leaves the button on TEST MIC", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    startMicTest.mockImplementation(() => Promise.reject(new Error("audio unavailable")));
    render(<Audio />);
    await userEvent.click(screen.getByRole("button", { name: /test mic/i }));
    await waitFor(() => expect(startMicTest).toHaveBeenCalled());
    await new Promise((r) => setTimeout(r, 0));
    expect(screen.getByRole("button", { name: /test mic/i })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /stop test/i })).not.toBeInTheDocument();
  });
});

// FE05: a drag commits live, throttled to 150ms, with a final commit on
// release -- not one config write per pointer-move frame.
describe("Audio drag cadence", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    setSettings.mockClear();
    usePendingSettings.setState({ pending: null });
    seed();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  const lastAudio = () => setSettings.mock.calls[setSettings.mock.calls.length - 1][0].audio;

  it("a 2s / 120-move MASTER drag calls setSettings at most 16 times and the last carries the release value", () => {
    render(<Audio />);
    const knob = screen.getByRole("slider", { name: /master/i });
    fireEvent.pointerDown(knob, { clientY: 1000 });
    for (let i = 1; i <= 120; i++) {
      act(() => {
        // 20px of upward travel in total: 75 -> 95 on the 0-100 scale.
        fireEvent.pointerMove(window, { clientY: 1000 - (i * 20) / 120 });
        vi.advanceTimersByTime(2000 / 120);
      });
    }
    act(() => {
      fireEvent.pointerUp(window);
    });
    act(() => {
      vi.advanceTimersByTime(500);
    });
    expect(setSettings.mock.calls.length).toBeGreaterThan(1);
    expect(setSettings.mock.calls.length).toBeLessThanOrEqual(16);
    expect(lastAudio().levels.master).toBeCloseTo(0.95, 5);
  });

  it("VOX threshold drag is throttled the same way", () => {
    useSettings.setState((st) => ({
      settings: st.settings && { ...st.settings, audio: { ...st.settings.audio, vox: true } },
    }));
    render(<Audio />);
    const range = screen.getByRole("slider", { name: /vox threshold/i });
    for (let i = 1; i <= 120; i++) {
      act(() => {
        fireEvent.change(range, { target: { value: String(Math.min(100, 35 + Math.floor(i / 2))) } });
        vi.advanceTimersByTime(2000 / 120);
      });
    }
    act(() => {
      fireEvent.pointerUp(range);
    });
    act(() => {
      vi.advanceTimersByTime(500);
    });
    expect(setSettings.mock.calls.length).toBeGreaterThan(1);
    expect(setSettings.mock.calls.length).toBeLessThanOrEqual(16);
    expect(lastAudio().vox_threshold).toBeCloseTo(0.95, 5);
  });

  it("keyboard auto-repeat on a knob is throttled and commits on keyup", () => {
    render(<Audio />);
    const knob = screen.getByRole("slider", { name: /master/i });
    for (let i = 0; i < 20; i++) {
      act(() => {
        fireEvent.keyDown(knob, { key: "ArrowUp" });
        vi.advanceTimersByTime(33);
      });
    }
    act(() => {
      fireEvent.keyUp(knob, { key: "ArrowUp" });
    });
    act(() => {
      vi.advanceTimersByTime(500);
    });
    // 20 presses over ~660ms: the leading send, about one per 150ms, and
    // the keyup commit -- far fewer than one per press.
    expect(setSettings.mock.calls.length).toBeLessThanOrEqual(7);
    expect(lastAudio().levels.master).toBeCloseTo(0.95, 5);
  });
});
