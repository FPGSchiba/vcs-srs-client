import { useEffect, useState } from "react";
import { api, type RadioDTO } from "../../shared/api/client";
import { useRadios } from "../../shared/store/radios";
import { LcdFreq } from "../../shared/components/LcdFreq";
import { khzToMhz, mhzToKhz } from "../../shared/freq";
import { variantById } from "./variants";
import { SHELLS } from "./shells";
import { nextRadioName } from "./radioName";
import { RadioFrame } from "./RadioFrame";
import { TalkerLine } from "./TalkerLine";
import { PttIndicator } from "./PttIndicator";
import { RadioSettings } from "./RadioSettings";

interface Props {
  radio: RadioDTO;
  allRadios: RadioDTO[];
  muted: boolean;
  /** The stored variant id. An unknown one resolves to the default. */
  variantId: string;
  /** The drawer size control; the parent owns where the choice goes. */
  onVariantChange: (variantId: string) => void;
}

/**
 * RadioCard resolves a variant descriptor, builds the card's pieces, and hands
 * them to the shell for that descriptor's orientation. It holds no geometry of
 * its own: adding a variant SIZE requires no change here.
 *
 * Editing is write-through: the edited radio is merged into `allRadios` to
 * build a full RadioInfoDTO and sent via api.updateRadioInfo. The store is NOT
 * updated optimistically — the server's `state:radio_update` echo is the
 * single source of truth for radio fields.
 *
 * Selection is different: a.st.SelectedRadio has no server echo (see
 * App.SelectRadio), so it IS applied optimistically to the shared store.
 *
 * Transmit state is derived: this radio's own PTT action held, or `global.ptt`
 * held with this radio as the FROZEN press-time target. Using live `selected`
 * there would let re-selecting mid-transmission retarget the indicator while
 * the backend keeps transmitting on whatever was selected at key-down.
 *
 * The frequency crosses one boundary here and nowhere else: the DTO carries an
 * MHz float, the LCD works in canonical integer kHz.
 */
export function RadioCard({ radio, allRadios, muted, variantId, onVariantChange }: Props) {
  const [editing, setEditing] = useState(false);
  const [name, setName] = useState(radio.name);
  const selectedRadioId = useRadios((s) => s.selectedRadioId);
  const heldPTT = useRadios((s) => s.heldPTT);
  const globalPttTargetId = useRadios((s) => s.globalPttTargetId);

  // Re-sync the draft when the upstream name changes (e.g. a server echo).
  useEffect(() => setName(radio.name), [radio.name]);

  const variant = variantById(variantId);
  const Shell = SHELLS[variant.orientation];

  function commit(next: RadioDTO) {
    void api.updateRadioInfo({ muted, radios: allRadios.map((r) => (r.id === next.id ? next : r)) });
  }

  function select() {
    useRadios.getState().setSelectedRadioId(radio.id);
    void api.selectRadio(radio.id);
  }

  const selected = selectedRadioId === radio.id;
  const disabled = !radio.enabled;
  // A disabled radio never transmits: the backend drops every frame for it
  // (resolveTXTarget returns nil when no ENABLED radio matches), yet
  // hotkey:pressed still fires, so an ungated held-PTT would light the full
  // transmitting treatment on a card that is drained and showing OFF.
  const transmitting =
    !disabled &&
    (heldPTT.has(`radio.${radio.id}.ptt`) ||
      (heldPTT.has("global.ptt") && globalPttTargetId === radio.id));
  const rid = `R${String(radio.id).padStart(2, "0")}`;

  return (
    <RadioFrame
      w={variant.w}
      h={variant.h}
      label={`${rid} ${radio.name}`}
      onSelect={select}
      selected={selected}
      receiving={false} /* no live per-radio RX feed yet — see the spec §7 */
      transmitting={transmitting}
      intercom={radio.is_intercom}
      disabled={disabled}
    >
      <Shell
        variant={variant}
        rid={
          <span
            className="cap mono"
            style={{
              color: radio.is_intercom ? "var(--ac-warn)" : "var(--ac-primary)",
              letterSpacing: "0.16em",
              flexShrink: 0,
            }}
          >
            {rid}
          </span>
        }
        name={
          editing ? (
            <input
              className="input"
              aria-label="radio name"
              autoFocus
              style={{ height: 20, fontSize: 12, padding: "0 6px", width: "100%" }}
              value={name}
              onChange={(e) => setName(e.target.value)}
              onBlur={() => {
                setEditing(false);
                setName(radio.name);
              }}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault();
                  setEditing(false);
                  const next = nextRadioName(name, radio.name);
                  if (next) commit({ ...radio, name: next });
                  else setName(radio.name);
                } else if (e.key === "Escape") {
                  e.preventDefault();
                  setName(radio.name);
                  setEditing(false);
                }
              }}
            />
          ) : (
            // A label, not a permanent bordered input: that input is the single
            // largest reason the shipped card read as a settings form.
            <button
              type="button"
              data-card-select // a single click still selects the card; see RadioFrame
              title="Double-click (or press Enter / Space / F2) to rename"
              onDoubleClick={() => setEditing(true)}
              // Double-click has no keyboard equivalent, so Enter / Space / F2
              // opens the editor too. preventDefault keeps the native
              // Enter/Space click from also selecting the card.
              onKeyDown={(e) => {
                if (e.target !== e.currentTarget) return;
                if (e.key === "Enter" || e.key === " " || e.key === "F2") {
                  e.preventDefault();
                  e.stopPropagation();
                  setEditing(true);
                }
              }}
              style={{
                color: disabled ? "var(--tx-4)" : "var(--tx-0)",
                fontSize: 12,
                display: "block",
                width: "100%",
                textAlign: "left",
                overflow: "hidden",
                textOverflow: "ellipsis",
                whiteSpace: "nowrap",
              }}
            >
              {radio.name}
            </button>
          )
        }
        lcd={
          <LcdFreq
            khz={mhzToKhz(radio.frequency)}
            digitPx={variant.lcdPx}
            unit={variant.shows.unit}
            onChange={(khz) => commit({ ...radio, frequency: khzToMhz(khz) })}
          />
        }
        talker={<TalkerLine disabled={disabled} self={transmitting} />}
        ptt={
          <PttIndicator
            w={variant.ptt.w}
            h={variant.ptt.h}
            transmitting={transmitting}
            showLabel={variant.shows.pttLabel}
          />
        }
        actions={
          <RadioSettings
            radio={radio}
            variantId={variant.id}
            onCommit={commit}
            onVariantChange={onVariantChange}
          />
        }
      />
    </RadioFrame>
  );
}
