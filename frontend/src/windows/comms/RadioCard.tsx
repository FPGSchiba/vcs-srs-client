import { useEffect, useState } from "react";
import { api, type RadioDTO } from "../../shared/api/client";
import { LcdFreq } from "../../shared/components/LcdFreq";
import { Toggle } from "../../shared/components/Toggle";
import { Icon } from "../../shared/components/Icon";

interface Props {
  radio: RadioDTO;
  allRadios: RadioDTO[];
  muted: boolean;
}

/**
 * RadioCard renders an editable radio strip ported from the design prototype's
 * `radio.jsx` RadioWidget. The frequency LCD, name, and enable/intercom toggles
 * are shown; the PTT button is display-only this phase.
 *
 * Editing is write-through: the name input holds transient local state while
 * focused, and on commit (blur / Enter, toggle click) the edited radio is merged
 * into `allRadios` to build a full `RadioInfoDTO` which is sent to the backend via
 * `api.updateRadioInfo`. The store is NOT updated optimistically — the server's
 * `state:radio_update` echo (handled by CommsApp) is the single source of truth.
 */
export function RadioCard({ radio, allRadios, muted }: Props) {
  const [name, setName] = useState(radio.name);

  // Re-sync local draft when the upstream radio name changes (e.g. server echo).
  useEffect(() => setName(radio.name), [radio.name]);

  function commit(next: RadioDTO) {
    const radios = allRadios.map((r) => (r.id === next.id ? next : r));
    void api.updateRadioInfo({ muted, radios });
  }

  return (
    <div className="radio">
      <div className="row acenter gap-3" style={{ minHeight: 22 }}>
        <span
          className="cap mono"
          style={{ color: "var(--ac-primary)", letterSpacing: "0.16em" }}
        >
          R{String(radio.id).padStart(2, "0")}
        </span>
        <input
          className="input"
          aria-label="radio name"
          style={{ height: 22, fontSize: 12, padding: "0 6px", flex: 1 }}
          value={name}
          onChange={(e) => setName(e.target.value)}
          onBlur={() => commit({ ...radio, name })}
          onKeyDown={(e) => {
            if (e.key === "Enter") e.currentTarget.blur();
          }}
        />
        {radio.is_intercom && (
          <span className="cap" style={{ color: "var(--ac-primary)" }}>
            ICOM
          </span>
        )}
      </div>

      <div className="row acenter between gap-4">
        <LcdFreq value={radio.frequency} />
        <div className="col gap-2" style={{ alignItems: "flex-end" }}>
          <label className="row acenter gap-2 cap mono" style={{ color: "var(--tx-3)" }}>
            ENABLED
            <Toggle
              on={radio.enabled}
              aria-label="toggle enabled"
              onChange={() => commit({ ...radio, enabled: !radio.enabled })}
            />
          </label>
          <label className="row acenter gap-2 cap mono" style={{ color: "var(--tx-3)" }}>
            INTERCOM
            <Toggle
              on={radio.is_intercom}
              aria-label="toggle intercom"
              onChange={() => commit({ ...radio, is_intercom: !radio.is_intercom })}
            />
          </label>
        </div>
      </div>

      <div className="row acenter gap-5" style={{ justifyContent: "space-between" }}>
        <button
          className="ptt"
          type="button"
          disabled
          style={{ flex: 1, maxWidth: 160, height: 48 }}
          title="Push to Talk (display-only this phase)"
        >
          <Icon name="mic" size={14} /> PUSH-TO-TALK
        </button>
      </div>
    </div>
  );
}
