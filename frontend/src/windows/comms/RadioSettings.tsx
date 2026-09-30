import { useEffect, useId, useLayoutEffect, useRef, useState } from "react";
import type { CSSProperties, ReactNode } from "react";
import { createPortal } from "react-dom";
import { Icon } from "../../shared/components/Icon";
import { Toggle } from "../../shared/components/Toggle";
import type { RadioDTO } from "../../shared/api/client";
import { VARIANTS } from "./variants";

interface Props {
  radio: RadioDTO;
  variantId: string;
  /** Write-through: the caller merges and sends; nothing is applied locally. */
  onCommit: (next: RadioDTO) => void;
  onVariantChange: (variantId: string) => void;
}

const WIDTH = 220;
const MARGIN = 8;
const GAP = 4;
/** Used until the panel can be measured (and where layout is not computed). */
const ESTIMATED_HEIGHT = 190;

/** A compact label + control row. Adding a setting is one more of these. */
function Row({ label, htmlFor, children }: { label: string; htmlFor?: string; children: ReactNode }) {
  return (
    <div className="row acenter" style={{ justifyContent: "space-between", gap: 10, minHeight: 24 }}>
      <label htmlFor={htmlFor} className="cap" style={{ color: "var(--tx-2)", fontSize: 10, letterSpacing: "0.1em" }}>
        {label}
      </label>
      {children}
    </div>
  );
}

/**
 * The per-radio settings gear and its drawer.
 *
 * The drawer PORTALS to document.body: RadioFrame is `overflow: hidden` and the
 * narrowest card is 150px wide, so anything rendered inside would be clipped.
 * It is positioned once from the gear's rect (below and right-aligned, flipped
 * above when it would leave the viewport) and CLOSED on scroll or resize
 * rather than tracked.
 *
 * Every click inside the drawer stops at the portal boundary: React bubbles
 * synthetic events through portals to the card's own select handler.
 */
export function RadioSettings({ radio, variantId, onCommit, onVariantChange }: Props) {
  const [open, setOpen] = useState(false);
  const [pos, setPos] = useState<{ left: number; top: number } | null>(null);
  const [name, setName] = useState(radio.name);
  const gearRef = useRef<HTMLButtonElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const uid = useId();
  const titleId = `${uid}-title`;
  const nameId = `${uid}-name`;

  useEffect(() => setName(radio.name), [radio.name]);

  function close() {
    setOpen(false);
    gearRef.current?.focus();
  }

  useLayoutEffect(() => {
    if (!open) {
      setPos(null);
      return;
    }
    const g = gearRef.current?.getBoundingClientRect();
    if (!g) return;
    const h = panelRef.current?.offsetHeight || ESTIMATED_HEIGHT;
    const vw = window.innerWidth;
    const vh = window.innerHeight;
    const left = Math.max(MARGIN, Math.min(g.right - WIDTH, vw - WIDTH - MARGIN));
    let top = g.bottom + GAP;
    if (top + h > vh - MARGIN) top = Math.max(MARGIN, g.top - GAP - h); // flip above
    setPos({ left, top });
    panelRef.current?.focus();
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: globalThis.PointerEvent) => {
      const t = e.target as Node | null;
      if (t && (panelRef.current?.contains(t) || gearRef.current?.contains(t))) return;
      close();
    };
    const dismiss = () => setOpen(false);
    document.addEventListener("pointerdown", onDown);
    window.addEventListener("scroll", dismiss, true);
    window.addEventListener("resize", dismiss);
    return () => {
      document.removeEventListener("pointerdown", onDown);
      window.removeEventListener("scroll", dismiss, true);
      window.removeEventListener("resize", dismiss);
    };
  }, [open]);

  function commitName() {
    const next = name.trim();
    if (next && next !== radio.name) onCommit({ ...radio, name: next });
    else setName(radio.name);
  }

  const stop = (e: { stopPropagation: () => void }) => e.stopPropagation();

  const panelStyle: CSSProperties = {
    position: "fixed",
    left: pos?.left ?? 0,
    top: pos?.top ?? 0,
    visibility: pos ? "visible" : "hidden",
    width: WIDTH,
    boxSizing: "border-box",
    background: "var(--bg-2)",
    border: "1px solid var(--bd-3)",
    borderRadius: 4,
    boxShadow: "0 12px 32px rgba(0,0,0,0.7)",
    zIndex: 100,
    padding: 8,
    display: "flex",
    flexDirection: "column",
    gap: 4,
    outline: "none",
  };

  return (
    <>
      <button
        ref={gearRef}
        type="button"
        aria-label="Radio settings"
        aria-haspopup="dialog"
        aria-expanded={open}
        title="Radio settings"
        onClick={(e) => {
          stop(e);
          setOpen((o) => !o);
        }}
        onDoubleClick={stop}
        style={{
          display: "inline-flex",
          alignItems: "center",
          justifyContent: "center",
          width: 16,
          height: 16,
          padding: 0,
          flexShrink: 0,
          border: 0,
          background: "transparent",
          color: open ? "var(--ac-primary)" : "var(--tx-3)",
          cursor: "pointer",
        }}
      >
        <Icon name="settings" size={12} />
      </button>
      {open &&
        createPortal(
          <div
            ref={panelRef}
            role="dialog"
            aria-labelledby={titleId}
            tabIndex={-1}
            style={panelStyle}
            onClick={stop}
            onDoubleClick={stop}
            onKeyDown={(e) => {
              if (e.key === "Escape") {
                e.preventDefault();
                e.stopPropagation();
                close();
              }
            }}
          >
            <div id={titleId} className="cap" style={{ color: "var(--tx-1)", fontSize: 10, letterSpacing: "0.14em" }}>
              Radio settings
            </div>
            <Row label="Name" htmlFor={nameId}>
              <input
                id={nameId}
                className="input"
                style={{ height: 20, fontSize: 12, padding: "0 6px", width: 120 }}
                value={name}
                onChange={(e) => setName(e.target.value)}
                onBlur={commitName}
                onKeyDown={(e) => {
                  if (e.key === "Enter") {
                    e.preventDefault();
                    commitName();
                  }
                }}
              />
            </Row>
            <Row label="Enabled">
              <Toggle
                on={radio.enabled}
                aria-label="enabled"
                onChange={(v) => onCommit({ ...radio, enabled: v })}
              />
            </Row>
            <Row label="Intercom">
              <Toggle
                on={radio.is_intercom}
                aria-label="intercom"
                onChange={(v) => onCommit({ ...radio, is_intercom: v })}
              />
            </Row>
            <fieldset style={{ border: 0, margin: 0, padding: 0, minWidth: 0 }}>
              <legend className="cap" style={{ color: "var(--tx-2)", fontSize: 10, letterSpacing: "0.1em", padding: 0 }}>
                Variant
              </legend>
              {VARIANTS.map((v) => (
                <label
                  key={v.id}
                  className="row acenter gap-2"
                  style={{ fontSize: 12, color: "var(--tx-1)", minHeight: 22, cursor: "pointer" }}
                >
                  <input
                    type="radio"
                    name={`${uid}-variant`}
                    checked={v.id === variantId}
                    onChange={() => onVariantChange(v.id)}
                  />
                  {v.label}
                </label>
              ))}
            </fieldset>
          </div>,
          document.body,
        )}
    </>
  );
}
