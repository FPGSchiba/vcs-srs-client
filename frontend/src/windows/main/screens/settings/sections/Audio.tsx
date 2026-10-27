import { useEffect, useRef, useState } from "react";
import { Panel } from "../../../../../shared/components/Panel";
import { SettingRow } from "../../../../../shared/components/SettingRow";
import { Toggle } from "../../../../../shared/components/Toggle";
import { Knob } from "../../../../../shared/components/Knob";
import { VU } from "../../../../../shared/components/VU";
import { Icon } from "../../../../../shared/components/Icon";
import { useThrottledCommit } from "../../../../../shared/hooks/useThrottledCommit";
import { api } from "../../../../../shared/api/client";
import { useSettings } from "../../../../../shared/store/settings";
import {
  commitSettings,
  useEffectiveSettings,
} from "../../../../../shared/store/settingsCommit";
import type {
  AudioSettings,
  AudioLevels,
  AudioDevice,
} from "../../../../../shared/store/settings";

// Levels and vox_threshold persist NORMALIZED 0-1 (see AudioSettingsDTO's Go
// doc); Knob and the VOX threshold slider both present the design's 0-100
// scale. This is the one place that conversion happens, on purpose -- see
// this file's own doc comment below.
const toKnob = (v: number): number => Math.round(v * 100);
const fromKnob = (v: number): number => v / 100;

/** Drag commit cadence (Phase 6.9 CL3, D2): live, at most once per 150ms. */
const DRAG_COMMIT_MS = 150;

/**
 * LevelKnob is one bus knob in the LEVELS panel. Its drag (and keyboard
 * auto-repeat) commits through its own throttle -- live, at most once per
 * DRAG_COMMIT_MS, plus a final commit on release -- instead of one
 * SetSettings (one config write and one broadcast to every window) per
 * pointer-move frame. Each commit patches only this bus onto the newest
 * in-flight settings, so concurrent changes are not lost.
 */
function LevelKnob({ bus, label, value }: { bus: keyof AudioLevels; label: string; value: number }) {
  const { push, flush } = useThrottledCommit<number>((v) => {
    void commitSettings((s) => ({
      ...s,
      audio: { ...s.audio, levels: { ...s.audio.levels, [bus]: fromKnob(v) } },
    }));
  }, DRAG_COMMIT_MS);
  return (
    <Knob
      value={toKnob(value)}
      onChange={push}
      onChangeEnd={flush}
      label={label}
      size={48}
    />
  );
}

/**
 * deviceOptions renders one <option> per enumerated device. `disambiguate`
 * overrides the accessible name (via aria-label) of the id==="" "System
 * Default" entry ONLY -- the visible text is untouched. Without it, the
 * Microphone and Speakers selects each render their own "System Default"
 * option with IDENTICAL accessible names, which is a genuine (if minor)
 * assistive-tech ambiguity: a screen-reader user navigating by role has no
 * way to tell which "System Default" belongs to which control. Passing a
 * distinguishing label for the second select's default entry fixes that
 * without changing what a sighted user sees in either dropdown.
 */
function deviceOptions(devices: AudioDevice[], disambiguateDefault?: string) {
  return devices.map((d) => (
    <option
      key={d.id}
      value={d.id}
      aria-label={d.id === "" && disambiguateDefault ? disambiguateDefault : undefined}
    >
      {d.name}
    </option>
  ));
}

/**
 * Audio renders the Settings -> "Audio & Sounds" section: DEVICES, LEVELS,
 * MIC TEST, PROCESSING, in that order. This panel order is an approved
 * deviation from the design prototype's SettingsAudio, which put the single
 * MASTER knob inside MIC TEST -- all four bus knobs now live in their own
 * LEVELS panel instead, leaving MIC TEST with just its button and VU meter.
 *
 * Go is the single source of truth, exactly like General.tsx: the store is
 * written only by the hydrate and the backend's `settings:changed` echo, and
 * every control goes through `commitSettings`, which patches `audio` on the
 * newest in-flight request and shows that request as `pending` until it
 * settles. A failed save clears `pending`, so the control returns to the
 * stored value; Go reports the failure through the notification channel
 * (Phase 6.9 CL3, D11). `testing` below is the one piece of local state, and
 * it holds only the mic-test button's own ephemeral UI state (mirroring
 * Keybinds.tsx's `capturingId`) -- it is never written back into
 * `settings.audio`. It turns on only once `startMicTest` has resolved, so a
 * failed start leaves the button on TEST MIC.
 */
export function Audio() {
  const settings = useEffectiveSettings();
  const devices = useSettings((s) => s.audioDevices);
  const vu = useSettings((s) => s.vu);
  const [testing, setTesting] = useState(false);
  // The VOX threshold slider's value while it is being dragged; null
  // otherwise. Committed through the same throttle as the knobs.
  const [voxDraft, setVoxDraft] = useState<number | null>(null);
  const voxCommit = useThrottledCommit<number>((v) => {
    void commitSettings((s) => ({ ...s, audio: { ...s.audio, vox_threshold: fromKnob(v) } }));
  }, DRAG_COMMIT_MS);
  const endVoxDrag = () => {
    if (voxDraft === null) return;
    voxCommit.flush(voxDraft);
    setVoxDraft(null);
  };

  // Mirrors `testing` for the unmount cleanup below without making the
  // effect re-run (and re-register) on every toggle.
  const testingRef = useRef(testing);
  testingRef.current = testing;

  // Stop a live mic test if the user navigates away from this section
  // mid-test -- otherwise mic passthrough stays engaged with StartMicTest's
  // override active and no control left on screen to turn it off.
  useEffect(() => {
    return () => {
      if (testingRef.current) api.stopMicTest().catch(console.error);
    };
  }, []);

  if (!settings) return null;
  const audio = settings.audio;

  // Patches are applied to the BASE commitSettings hands in (the newest
  // in-flight request, else the store), never to this render's `audio`.
  const updateAudio = (patch: (a: AudioSettings) => Partial<AudioSettings>) => {
    void commitSettings((s) => ({ ...s, audio: { ...s.audio, ...patch(s.audio) } }));
  };
  const update = (patch: Partial<AudioSettings>) => updateAudio(() => patch);

  const selectDevice = (kind: "input" | "output", id: string) => {
    const list = kind === "input" ? devices.inputs : devices.outputs;
    const name = list.find((d) => d.id === id)?.name ?? "";
    update(
      kind === "input"
        ? { input_device: id, input_device_name: name }
        : { output_device: id, output_device_name: name },
    );
  };

  const toggleMicTest = () => {
    if (testing) {
      api
        .stopMicTest()
        .catch(console.error)
        .finally(() => setTesting(false));
    } else {
      api
        .startMicTest()
        .then(() => setTesting(true))
        .catch(console.error);
    }
  };

  return (
    <div className="col gap-5">
      <Panel title="DEVICES">
        <SettingRow
          label="Microphone"
          desc={audio.input_device_name || "System Default"}
          control={
            <select
              aria-label="Microphone"
              className="input"
              style={{ width: 240 }}
              value={audio.input_device}
              onChange={(e) => selectDevice("input", e.target.value)}
            >
              {deviceOptions(devices.inputs)}
            </select>
          }
        />
        <SettingRow
          label="Speakers / Headphones"
          desc={audio.output_device_name || "System Default"}
          control={
            <select
              aria-label="Speakers / Headphones"
              className="input"
              style={{ width: 240 }}
              value={audio.output_device}
              onChange={(e) => selectDevice("output", e.target.value)}
            >
              {deviceOptions(devices.outputs, "System Default (output)")}
            </select>
          }
        />
        <SettingRow
          label="Mic passthrough"
          desc="Mix mic audio into your own output."
          control={
            <Toggle
              on={audio.mic_passthrough}
              onChange={(v) => update({ mic_passthrough: v })}
              lg
              aria-label="Mic passthrough"
            />
          }
        />
      </Panel>

      <Panel title="LEVELS">
        <div className="row gap-6" style={{ flexWrap: "wrap" }}>
          <LevelKnob bus="master" label="MASTER" value={audio.levels.master} />
          <LevelKnob bus="voice" label="VOICE" value={audio.levels.voice} />
          <LevelKnob bus="sfx" label="SFX" value={audio.levels.sfx} />
          <LevelKnob bus="notification" label="NOTIFICATION" value={audio.levels.notification} />
        </div>
      </Panel>

      <Panel title="MIC TEST">
        <div className="row acenter gap-6">
          <button className="btn btn-primary" type="button" onClick={toggleMicTest}>
            <Icon name="mic" size={11} /> {testing ? "STOP TEST" : "TEST MIC"}
          </button>
          <div className="col gap-2" style={{ flex: 1 }}>
            <span className="cap-dim">Live input</span>
            <VU level={vu.input} segs={16} aria-label="Live input level" />
          </div>
        </div>
      </Panel>

      <Panel title="PROCESSING">
        <SettingRow
          label="Automatic Gain Control (AGC)"
          control={
            <Toggle
              on={audio.agc}
              onChange={(v) => update({ agc: v })}
              lg
              aria-label="Automatic Gain Control (AGC)"
            />
          }
        />
        <SettingRow
          label="Noise Suppression"
          control={
            <Toggle
              on={audio.noise_suppression}
              onChange={(v) => update({ noise_suppression: v })}
              lg
              aria-label="Noise Suppression"
            />
          }
        />
        <SettingRow
          label="Voice activation (VOX)"
          desc="Open channel when mic level passes threshold."
          control={
            <Toggle
              on={audio.vox}
              onChange={(v) => update({ vox: v })}
              lg
              aria-label="Voice activation (VOX)"
            />
          }
        />
        {audio.vox && (
          <>
            <SettingRow
              label="VOX threshold"
              control={
                <input
                  type="range"
                  min={0}
                  max={100}
                  value={voxDraft ?? toKnob(audio.vox_threshold)}
                  aria-label="VOX threshold"
                  style={{ width: 200 }}
                  onChange={(e) => {
                    const v = Number(e.target.value);
                    setVoxDraft(v);
                    voxCommit.push(v);
                  }}
                  onPointerUp={endVoxDrag}
                  onPointerCancel={endVoxDrag}
                  onKeyUp={endVoxDrag}
                  onBlur={endVoxDrag}
                />
              }
            />
            <SettingRow
              label="VOX min length"
              desc="Minimum sustain time before triggering."
              control={
                <input
                  type="number"
                  className="input mono"
                  aria-label="VOX min length (ms)"
                  style={{ width: 100 }}
                  value={audio.vox_min_length_ms}
                  onChange={(e) => update({ vox_min_length_ms: Number(e.target.value) })}
                />
              }
            />
            <SettingRow
              label="VOX noise cancel"
              control={
                <Toggle
                  on={audio.vox_noise_cancel}
                  onChange={(v) => update({ vox_noise_cancel: v })}
                  lg
                  aria-label="VOX noise cancel"
                />
              }
            />
          </>
        )}
        <SettingRow
          label="PTT start delay"
          desc="Pause before mic opens after key down."
          control={
            <input
              type="number"
              className="input mono"
              aria-label="PTT start delay (ms)"
              style={{ width: 100 }}
              value={audio.ptt_start_delay_ms}
              onChange={(e) => update({ ptt_start_delay_ms: Number(e.target.value) })}
            />
          }
        />
        <SettingRow
          label="PTT release delay"
          desc="Tail before mic closes after key up."
          control={
            <input
              type="number"
              className="input mono"
              aria-label="PTT release delay (ms)"
              style={{ width: 100 }}
              value={audio.ptt_release_delay_ms}
              onChange={(e) => update({ ptt_release_delay_ms: Number(e.target.value) })}
            />
          }
        />
      </Panel>
    </div>
  );
}
