import { useState } from "react";
import { Panel } from "../../../../../shared/components/Panel";
import { SettingRow } from "../../../../../shared/components/SettingRow";
import { Toggle } from "../../../../../shared/components/Toggle";
import { Icon } from "../../../../../shared/components/Icon";
import { api } from "../../../../../shared/api/client";
import { useSettings } from "../../../../../shared/store/settings";
import type { AudioEffect, AudioSettings } from "../../../../../shared/store/settings";

interface SampleSlot {
  id: string;
  label: string;
}

/**
 * The seven sample-backed Radio Effects slots, mirroring
 * internal/audio/assets/manifest.toml's slot ids and labels one for one.
 *
 * `AudioEffectDTO.Label` from the backend is a placeholder today -- it
 * echoes the slot id verbatim (see audioSettingsDTO's doc comment in
 * internal/app/audio.go), because Manager exposes no accessor for the SFX
 * manifest's real label yet and this task is scoped to frontend/src only.
 * Rendering that would show "tx_start" instead of "TX Start", so these
 * labels are hardcoded here from the manifest instead of read off the DTO.
 * A future task that adds the Manager accessor mentioned in that doc should
 * source these from the backend and delete this list.
 */
const SAMPLE_SLOTS: SampleSlot[] = [
  { id: "tx_start", label: "TX Start" },
  { id: "tx_end", label: "TX End" },
  { id: "rx_start", label: "RX Start" },
  { id: "rx_end", label: "RX End" },
  { id: "intercom_start", label: "Intercom Start" },
  { id: "intercom_end", label: "Intercom End" },
  { id: "encryption_beep", label: "Encryption Beep" },
];

/**
 * `audio.effects` is nil/empty until the backend or a user touches a slot
 * (see config.Default's doc for why it starts nil). A slot missing from the
 * map renders exactly like one the backend reports as unavailable: off, no
 * file, PREVIEW disabled. This is the honest state, not a guess.
 */
const EMPTY_EFFECT: AudioEffect = { enabled: false, file: "", label: "", available: false };

interface Preset {
  value: string;
  label: string;
}

/**
 * voice_effect / clipping_effect are BUILT-IN NAMED DSP CONFIGURATIONS, not
 * sample files -- see internal/audio/effect.go's `Effect` doc (spec D12).
 * The design prototype writes them with `.preset` extensions, but there is
 * no preset file format or parser: the "filename" is just an identifier.
 *
 * Task 17 is scoped to frontend/src only, so there is no bound accessor for
 * internal/audio's `VoicePresets()` / `ClippingPresets()`. These lists are
 * hardcoded here and must mirror those functions one-for-one -- update both
 * places if a preset is ever added, renamed, or removed.
 */
const VOICE_PRESETS: Preset[] = [
  { value: "", label: "Off" },
  { value: "comms_filter_low", label: "Comms Filter (Low)" },
  { value: "comms_filter_mid", label: "Comms Filter (Mid)" },
  { value: "comms_filter_high", label: "Comms Filter (High)" },
];

const CLIPPING_PRESETS: Preset[] = [
  { value: "", label: "Off" },
  { value: "soft_limit", label: "Soft Limit" },
  { value: "saturated_overdrive", label: "Saturated Overdrive" },
];

/**
 * Effects renders Settings -> "Radio Effects", replacing the `Deferred`
 * stub. Nine rows, two kinds -- conflating them would be wrong in a way
 * that looks right:
 *
 * - Seven SAMPLE slots (this file's `SAMPLE_SLOTS`), each a label + sample
 *   select + PREVIEW button + enable toggle, mapping to
 *   `audio.effects[id]`.
 * - Two DSP PRESET rows (`voice_effect`, `clipping_effect`), each a select
 *   of preset NAMES (not filenames), mapping to the matching top-level
 *   `AudioSettings` string field. No file, no PREVIEW -- there is nothing
 *   to play, only a processing chain to toggle.
 *
 * Same single-source-of-truth flow as Audio.tsx and General.tsx: every
 * control calls `api.setSettings` with the full struct (audio patched in)
 * and never writes the store itself -- a row only re-renders once the
 * backend's `settings:changed` event lands and useSettingsSync's
 * subscription writes the new struct into the store. `previewingId` is the
 * one piece of local state, and it holds only the PREVIEW button's own
 * ephemeral in-flight UI feedback -- it is never written back into
 * `settings.audio`.
 *
 * The SFX sample pack does not exist yet -- `internal/audio/assets/`
 * contains only a manifest and a README -- so every slot's `available` is
 * false today. PREVIEW is disabled whenever a slot is unavailable: a button
 * that looks live and plays silence is worse than one that is honestly
 * inert, because it makes the user think the app is broken rather than
 * that content is missing.
 */
export function Effects() {
  const settings = useSettings((s) => s.settings);
  const [previewingId, setPreviewingId] = useState<string | null>(null);

  if (!settings) return null;
  const audio = settings.audio;

  const update = (patch: Partial<AudioSettings>) => {
    void api.setSettings({ ...settings, audio: { ...audio, ...patch } });
  };

  const updateEffect = (id: string, patch: Partial<AudioEffect>) => {
    const current = audio.effects[id] ?? EMPTY_EFFECT;
    update({ effects: { ...audio.effects, [id]: { ...current, ...patch } } });
  };

  const preview = async (id: string) => {
    setPreviewingId(id);
    try {
      await api.previewEffect(id);
    } finally {
      setPreviewingId((cur) => (cur === id ? null : cur));
    }
  };

  return (
    <Panel title="RADIO EFFECTS">
      {SAMPLE_SLOTS.map((slot) => {
        const effect = audio.effects[slot.id] ?? EMPTY_EFFECT;
        return (
          <SettingRow
            key={slot.id}
            label={slot.label}
            control={
              <div className="row gap-3 acenter">
                <select
                  aria-label={`${slot.label} sample`}
                  className="input mono"
                  style={{ width: 200 }}
                  value={effect.file}
                  onChange={(e) => updateEffect(slot.id, { file: e.target.value })}
                >
                  <option value={effect.file}>{effect.file || "No sample"}</option>
                </select>
                <button
                  className="btn btn-sm"
                  type="button"
                  disabled={!effect.available}
                  aria-label={`Preview ${slot.label}`}
                  onClick={() => void preview(slot.id)}
                >
                  <Icon name="volume" size={11} /> {previewingId === slot.id ? "..." : "PREVIEW"}
                </button>
                <Toggle
                  on={effect.enabled}
                  onChange={(v) => updateEffect(slot.id, { enabled: v })}
                  aria-label={`Enable ${slot.label}`}
                />
              </div>
            }
          />
        );
      })}
      <SettingRow
        label="Voice Effect"
        desc="Radio bandpass coloration applied to outgoing voice."
        control={
          <select
            aria-label="Voice effect"
            className="input"
            style={{ width: 220 }}
            value={audio.voice_effect}
            onChange={(e) => update({ voice_effect: e.target.value })}
          >
            {VOICE_PRESETS.map((p) => (
              <option key={p.value} value={p.value}>
                {p.label}
              </option>
            ))}
          </select>
        }
      />
      <SettingRow
        label="Clipping Effect"
        desc="Soft-clip drive applied to outgoing voice."
        control={
          <select
            aria-label="Clipping effect"
            className="input"
            style={{ width: 220 }}
            value={audio.clipping_effect}
            onChange={(e) => update({ clipping_effect: e.target.value })}
          >
            {CLIPPING_PRESETS.map((p) => (
              <option key={p.value} value={p.value}>
                {p.label}
              </option>
            ))}
          </select>
        }
      />
    </Panel>
  );
}
