import { useState } from "react";
import { Panel } from "../../../../../shared/components/Panel";
import { SettingRow } from "../../../../../shared/components/SettingRow";
import { Toggle } from "../../../../../shared/components/Toggle";
import { Icon } from "../../../../../shared/components/Icon";
import { api } from "../../../../../shared/api/client";
import { useSettings } from "../../../../../shared/store/settings";
import type { AudioEffect, AudioSettings } from "../../../../../shared/store/settings";

/**
 * `audio.effects` is nil/empty until the backend or a user touches a slot
 * (see config.Default's doc for why it starts nil). A slot missing from the
 * map renders exactly like one the backend reports as unavailable: off, no
 * file, PREVIEW disabled. This is the honest state, not a guess.
 */
const EMPTY_EFFECT: AudioEffect = { enabled: false, file: "", label: "", available: false };

/**
 * Effects renders Settings -> "Radio Effects", replacing the `Deferred`
 * stub. Nine rows, two kinds -- conflating them would be wrong in a way
 * that looks right:
 *
 * - The manifest's SAMPLE slots (`settings.audio.effect_order`, one
 *   `SettingRow` per id), each a label + sample select + PREVIEW button +
 *   enable toggle, mapping to `audio.effects[id]`. The id set, display
 *   order, and label all come from the backend's SFX manifest (see
 *   `AudioSettingsDTO.EffectOrder`'s Go doc) -- this file has no hardcoded
 *   copy of internal/audio/assets/manifest.toml to keep in sync.
 * - Two DSP PRESET rows (`voice_effect`, `clipping_effect`), each a select
 *   of preset NAMES (not filenames) sourced from `getAudioEffectPresets()`
 *   (the store's `audioEffectPresets`, hydrated once by useSettingsSync),
 *   mapping to the matching top-level `AudioSettings` string field. No
 *   file, no PREVIEW -- there is nothing to play, only a processing chain
 *   to toggle.
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
  const presets = useSettings((s) => s.audioEffectPresets);
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
      {audio.effect_order.map((id) => {
        const effect = audio.effects[id] ?? EMPTY_EFFECT;
        const label = effect.label || id;
        return (
          <SettingRow
            key={id}
            label={label}
            control={
              <div className="row gap-3 acenter">
                <select
                  aria-label={`${label} sample`}
                  className="input mono"
                  style={{ width: 200 }}
                  value={effect.file}
                  onChange={(e) => updateEffect(id, { file: e.target.value })}
                >
                  <option value={effect.file}>{effect.file || "No sample"}</option>
                </select>
                <button
                  className="btn btn-sm"
                  type="button"
                  disabled={!effect.available}
                  aria-label={`Preview ${label}`}
                  onClick={() => void preview(id)}
                >
                  <Icon name="volume" size={11} /> {previewingId === id ? "..." : "PREVIEW"}
                </button>
                <Toggle
                  on={effect.enabled}
                  onChange={(v) => updateEffect(id, { enabled: v })}
                  aria-label={`Enable ${label}`}
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
            {presets.voice.map((p) => (
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
            {presets.clipping.map((p) => (
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
