import { useEffect, useState } from "react";
import { api, type RadioDTO } from "../../shared/api/client";
import { useRadios } from "../../shared/store/radios";
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
 * are shown, plus selection and live transmit state.
 *
 * Editing is write-through: the name input holds transient local state while
 * focused, and on commit (blur / Enter, toggle click, LCD edit) the edited radio
 * is merged into `allRadios` to build a full `RadioInfoDTO` which is sent to the
 * backend via `api.updateRadioInfo`. The store is NOT updated optimistically for
 * these fields — the server's `state:radio_update` echo (handled by CommsApp) is
 * the single source of truth.
 *
 * Selection (clicking the card) and transmit state are different: selection is
 * pure client-local UI state (a.st.SelectedRadio has no server echo — see
 * App.SelectRadio), so it IS updated optimistically via the shared `useRadios`
 * store, which CommsApp also feeds from api.voiceState() on mount. Transmit
 * state is derived, not stored per-radio: `radio.<id>.ptt` held, or
 * `global.ptt` held while this radio is selected (holding both is a set union —
 * see App.resolveTXTarget/txPress in internal/app/voice.go).
 */
export function RadioCard({ radio, allRadios, muted }: Props) {
  const [name, setName] = useState(radio.name);
  const selectedRadioId = useRadios((s) => s.selectedRadioId);
  const heldPTT = useRadios((s) => s.heldPTT);

  // Re-sync local draft when the upstream radio name changes (e.g. server echo).
  useEffect(() => setName(radio.name), [radio.name]);

  function commit(next: RadioDTO) {
    const radios = allRadios.map((r) => (r.id === next.id ? next : r));
    void api.updateRadioInfo({ muted, radios });
  }

  function select() {
    useRadios.getState().setSelectedRadioId(radio.id);
    void api.selectRadio(radio.id);
  }

  const selected = selectedRadioId === radio.id;
  const transmitting =
    heldPTT.has(`radio.${radio.id}.ptt`) || (selected && heldPTT.has("global.ptt"));

  return (
    <div
      className="radio"
      onClick={select}
      aria-selected={selected}
      style={{
        border: `1px solid ${selected ? "var(--ac-primary)" : "var(--bd-2)"}`,
        cursor: "pointer",
      }}
    >
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
          onClick={(e) => e.stopPropagation()}
        />
        {radio.is_intercom && (
          <span className="cap" style={{ color: "var(--ac-primary)" }}>
            ICOM
          </span>
        )}
      </div>

      <div className="row acenter between gap-4" onClick={(e) => e.stopPropagation()}>
        <LcdFreq
          value={radio.frequency}
          onChange={(frequency) => commit({ ...radio, frequency })}
        />
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
          className={`ptt ${transmitting ? "keyed" : ""}`.trim()}
          type="button"
          style={{ flex: 1, maxWidth: 160, height: 48 }}
          title="Push to Talk"
          onClick={(e) => e.stopPropagation()}
        >
          <Icon name="mic" size={14} /> {transmitting ? "TRANSMIT" : "PUSH-TO-TALK"}
        </button>
      </div>
    </div>
  );
}
