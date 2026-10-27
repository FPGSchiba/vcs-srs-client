import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

const setSettings = vi.fn();
vi.mock("../api/client", () => ({
  api: { setSettings: (s: unknown) => setSettings(s) },
}));

import { useSettings, type Settings } from "./settings";
import { commitSettings, usePendingSettings } from "./settingsCommit";

const base: Settings = {
  start_minimized: false, minimize_to_tray: true, show_transmitter_name: true,
  play_connection_sounds: true, play_notification_sounds: true, radio_switch_as_ptt: false,
  audio: {
    input_device: "", output_device: "", input_device_name: "", output_device_name: "",
    mic_passthrough: false, agc: true, noise_suppression: true,
    vox: false, vox_threshold: 0.35, vox_min_length_ms: 220, vox_hang_ms: 300,
    vox_noise_cancel: true, ptt_start_delay_ms: 0, ptt_release_delay_ms: 120,
    voice_effect: "", clipping_effect: "",
    levels: { master: 0.75, voice: 1, sfx: 0.8, notification: 0.8 },
    effects: {},
    effect_order: [],
  },
} as Settings;

function deferred() {
  let resolve!: () => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<void>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

const setLevel = (bus: "master" | "voice", v: number) => (s: Settings): Settings => ({
  ...s,
  audio: { ...s.audio, levels: { ...s.audio.levels, [bus]: v } },
});

describe("commitSettings", () => {
  beforeEach(() => {
    setSettings.mockReset();
    useSettings.setState({ settings: base });
    usePendingSettings.setState({ pending: null });
  });
  afterEach(() => vi.restoreAllMocks());

  it("two commits before any echo both survive", () => {
    setSettings.mockReturnValue(new Promise(() => {}));
    void commitSettings(setLevel("master", 0.5));
    void commitSettings(setLevel("voice", 0.2));
    expect(setSettings).toHaveBeenCalledTimes(2);
    const second = setSettings.mock.calls[1][0] as Settings;
    expect(second.audio.levels.master).toBe(0.5);
    expect(second.audio.levels.voice).toBe(0.2);
  });

  it("rejection clears pending so the effective value is the store again", async () => {
    const err = vi.spyOn(console, "error").mockImplementation(() => {});
    setSettings.mockRejectedValue(new Error("read-only"));
    const p = commitSettings((s) => ({ ...s, start_minimized: true }));
    expect(usePendingSettings.getState().pending?.start_minimized).toBe(true);
    await p;
    expect(usePendingSettings.getState().pending).toBeNull();
    expect(err).toHaveBeenCalled();
  });

  it("a synchronous throw from setSettings is a handled rejection that rolls back", async () => {
    const err = vi.spyOn(console, "error").mockImplementation(() => {});
    const boom = new Error("binding missing");
    setSettings.mockImplementation(() => {
      throw boom;
    });
    const p = commitSettings((s) => ({ ...s, start_minimized: true }));
    // The request was issued in the same tick and the overlay is shown.
    expect(setSettings).toHaveBeenCalledTimes(1);
    expect(usePendingSettings.getState().pending?.start_minimized).toBe(true);
    await expect(p).resolves.toBeUndefined();
    expect(usePendingSettings.getState().pending).toBeNull();
    expect(err).toHaveBeenCalledWith(expect.any(String), boom);
  });

  it("a settle of an older commit keeps the newer pending", async () => {
    const a = deferred();
    const b = deferred();
    setSettings.mockReturnValueOnce(a.promise).mockReturnValueOnce(b.promise);
    const pa = commitSettings(setLevel("master", 0.5));
    void commitSettings(setLevel("voice", 0.2));
    const newer = usePendingSettings.getState().pending;
    a.resolve();
    await pa;
    expect(usePendingSettings.getState().pending).toBe(newer);
    expect(newer?.audio.levels.voice).toBe(0.2);
  });

  it("does nothing before the store has settings", async () => {
    useSettings.setState({ settings: null });
    await commitSettings((s) => s);
    expect(setSettings).not.toHaveBeenCalled();
  });
});
