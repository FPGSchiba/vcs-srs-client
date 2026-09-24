import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { Effects } from "./Effects";
import { useSettings } from "../../../../../shared/store/settings";
import type { AudioEffect } from "../../../../../shared/store/settings";

const setSettings = vi.fn();
const previewEffect = vi.fn((_id: string) => Promise.resolve());
vi.mock("../../../../../shared/api/client", () => ({
  api: {
    setSettings: (...a: unknown[]) => setSettings(...a),
    previewEffect: (id: string) => previewEffect(id),
  },
}));

// Mirrors internal/audio/assets/manifest.toml's seven slot ids and labels,
// and effect_order mirrors the manifest's `order` field -- see
// AudioSettingsDTO.EffectOrder's Go doc for why order travels as its own
// array rather than object key order. `available: false` on every entry is
// the DEFAULT case exercised here on purpose -- it's the state the app
// genuinely ships in until the SFX sample pack (internal/audio/assets/,
// currently just a manifest + README) lands. See AudioEffectDTO's Go doc.
function unavailableEffect(label: string, file: string): AudioEffect {
  return { enabled: true, file, label, available: false };
}

const EFFECT_ORDER = [
  "tx_start", "tx_end", "rx_start", "rx_end", "intercom_start", "intercom_end", "encryption_beep",
];

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
        effect_order: EFFECT_ORDER,
        effects: {
          tx_start: unavailableEffect("TX Start", "transmit_open.wav"),
          tx_end: unavailableEffect("TX End", "transmit_close.wav"),
          rx_start: unavailableEffect("RX Start", "receive_open.wav"),
          rx_end: unavailableEffect("RX End", "receive_close.wav"),
          intercom_start: unavailableEffect("Intercom Start", "intercom_open.wav"),
          intercom_end: unavailableEffect("Intercom End", "intercom_close.wav"),
          encryption_beep: unavailableEffect("Encryption Beep", "crypto_handshake.wav"),
        },
      },
    },
    audioDevices: { inputs: [], outputs: [] },
    audioEffectPresets: {
      voice: [
        { value: "", label: "Off" },
        { value: "comms_filter_low", label: "Comms Filter (Low)" },
        { value: "comms_filter_mid", label: "Comms Filter (Mid)" },
        { value: "comms_filter_high", label: "Comms Filter (High)" },
      ],
      clipping: [
        { value: "", label: "Off" },
        { value: "soft_limit", label: "Soft Limit" },
        { value: "saturated_overdrive", label: "Saturated Overdrive" },
      ],
    },
  });
}

describe("Radio Effects settings", () => {
  beforeEach(() => {
    setSettings.mockClear();
    previewEffect.mockClear();
    seed();
  });

  it("renders a row per manifest slot plus the two preset rows", () => {
    render(<Effects />);
    // Assert all seven manifest slots, not just the first/last -- the
    // component renders generically over effect_order.map, so a mutation
    // special-casing just the endpoints would otherwise slip through.
    const wantLabels = [
      "TX Start", "TX End", "RX Start", "RX End",
      "Intercom Start", "Intercom End", "Encryption Beep",
    ];
    for (const label of wantLabels) {
      expect(screen.getByText(label)).toBeInTheDocument();
    }
    expect(screen.getAllByRole("button", { name: /^preview /i })).toHaveLength(wantLabels.length);
    expect(screen.getByLabelText(/voice effect/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/clipping effect/i)).toBeInTheDocument();
  });

  it("disables PREVIEW for a slot with no sample behind it", () => {
    render(<Effects />);
    expect(screen.getByRole("button", { name: /preview tx start/i })).toBeDisabled();
  });

  it("enables PREVIEW once a slot's sample is available and wires it to previewEffect", async () => {
    seed();
    useSettings.setState((state) => ({
      settings: state.settings && {
        ...state.settings,
        audio: {
          ...state.settings.audio,
          effects: {
            ...state.settings.audio.effects,
            tx_start: { ...state.settings.audio.effects.tx_start, available: true },
          },
        },
      },
    }));
    render(<Effects />);
    const button = screen.getByRole("button", { name: /preview tx start/i });
    expect(button).toBeEnabled();
    await userEvent.click(button);
    expect(previewEffect).toHaveBeenCalledWith("tx_start");
  });

  it("persists a preset change through setSettings", async () => {
    render(<Effects />);
    await userEvent.selectOptions(screen.getByLabelText(/voice effect/i), "comms_filter_high");
    await waitFor(() => expect(setSettings).toHaveBeenCalled());
    expect(setSettings.mock.calls[0][0].audio.voice_effect).toBe("comms_filter_high");
  });

  it("persists an enable-toggle change for a sample slot without touching other slots", async () => {
    render(<Effects />);
    await userEvent.click(screen.getByLabelText(/enable tx start/i));
    await waitFor(() => expect(setSettings).toHaveBeenCalled());
    const patched = setSettings.mock.calls[0][0].audio.effects;
    expect(patched.tx_start.enabled).toBe(false);
    expect(patched.tx_end.enabled).toBe(true);
  });
});
