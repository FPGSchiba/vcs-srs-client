import { useEffect, useRef, useState } from "react";
import { Panel } from "../../../../../shared/components/Panel";
import { SettingRow } from "../../../../../shared/components/SettingRow";
import { Toggle } from "../../../../../shared/components/Toggle";
import { Knob } from "../../../../../shared/components/Knob";
import { VU } from "../../../../../shared/components/VU";
import { Icon } from "../../../../../shared/components/Icon";
import { api } from "../../../../../shared/api/client";
import { useSettings } from "../../../../../shared/store/settings";
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
 * Go is the single source of truth, exactly like General.tsx: every control
 * calls `api.setSettings` with the full settings struct (audio patched in)
 * and never writes the store itself -- a row only re-renders once the
 * backend's `settings:changed` event lands and useSettingsSync's
 * subscription writes the new struct into the store. There is no local
 * mirror of any PERSISTED field here. `testing` below is the one piece of
 * local state, and it holds only the mic-test button's own ephemeral UI
 * state (mirroring Keybinds.tsx's `capturingId`) -- it is never written back
 * into `settings.audio`.
 */
export function Audio() {
  const settings = useSettings((s) => s.settings);
  const devices = useSettings((s) => s.audioDevices);
  const vu = useSettings((s) => s.vu);
  const [testing, setTesting] = useState(false);

  // Mirrors `testing` for the unmount cleanup below without making the
  // effect re-run (and re-register) on every toggle.
  const testingRef = useRef(testing);
  testingRef.current = testing;

  // Stop a live mic test if the user navigates away from this section
  // mid-test -- otherwise mic passthrough stays engaged with StartMicTest's
  // override active and no control left on screen to turn it off.
  useEffect(() => {
    return () => {
      if (testingRef.current) void api.stopMicTest();
    };
  }, []);

  if (!settings) return null;
  const audio = settings.audio;

  const update = (patch: Partial<AudioSettings>) => {
    void api.setSettings({ ...settings, audio: { ...audio, ...patch } });
  };

  const updateLevel = (key: keyof AudioLevels, knobValue: number) => {
    update({ levels: { ...audio.levels, [key]: fromKnob(knobValue) } });
  };

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
      void api.stopMicTest();
      setTesting(false);
    } else {
      void api.startMicTest();
      setTesting(true);
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
          <Knob
            value={toKnob(audio.levels.master)}
            onChange={(v) => updateLevel("master", v)}
            label="MASTER"
            size={48}
          />
          <Knob
            value={toKnob(audio.levels.voice)}
            onChange={(v) => updateLevel("voice", v)}
            label="VOICE"
            size={48}
          />
          <Knob
            value={toKnob(audio.levels.sfx)}
            onChange={(v) => updateLevel("sfx", v)}
            label="SFX"
            size={48}
          />
          <Knob
            value={toKnob(audio.levels.notification)}
            onChange={(v) => updateLevel("notification", v)}
            label="NOTIFICATION"
            size={48}
          />
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
                  value={toKnob(audio.vox_threshold)}
                  aria-label="VOX threshold"
                  style={{ width: 200 }}
                  onChange={(e) => update({ vox_threshold: fromKnob(Number(e.target.value)) })}
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
