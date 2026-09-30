import { useEffect, useId, useLayoutEffect, useRef, useState } from "react";
import type { CSSProperties, ReactNode } from "react";
import { createPortal } from "react-dom";
import { Icon } from "../../shared/components/Icon";
import { Toggle } from "../../shared/components/Toggle";
import type { RadioDTO } from "../../shared/api/client";
import { VARIANTS } from "./variants";
import { nextRadioName } from "./radioName";

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
function Row({ label, children }: { label: string; children: ReactNode }) {
  // A <label> wrapping its control: clicking the visible text operates it.
  return (
    <label className="row acenter" style={{ justifyContent: "space-between", gap: 10, minHeight: 24 }}>
      <span className="cap" style={{ color: "var(--tx-2)", fontSize: 10, letterSpacing: "0.1em" }}>
        {label}
      </span>
      {children}
    </label>
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
 * Click and drag events inside the drawer stop at the portal boundary: React
 * bubbles synthetic events through portals to the card's select handler and to
 * RadioBlock's HTML5 reorder handlers.
 *
 * Choosing a variant closes the drawer first. A same-orientation change moves
 * the gear out from under a drawer positioned once, and a different-orientation
 * change swaps the shell component and unmounts this one outright. KNOWN
 * LIMITATION: after an orientation change the gear belongs to a freshly mounted
 * shell, so the focus return below may not land; it is on the manual checklist.
 *
 * Focus moves into the panel in an effect keyed on `pos`, i.e. after the
 * visible render has committed: a browser will not focus a `visibility:hidden`
 * element, and the first pass is hidden until it is measured. Escape is
 * handled on the document so dismissal never depends on where focus is.
 */
export function RadioSettings({ radio, variantId, onCommit, onVariantChange }: Props) {
  const [open, setOpen] = useState(false);
  const [pos, setPos] = useState<{ left: number; top: number } | null>(null);
  const [name, setName] = useState(radio.name);
  const gearRef = useRef<HTMLButtonElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const uid = useId();
  const titleId = `${uid}-title`;
  // What the server last accepted or we last sent: Enter then blur must not
  // send the same name twice while the echo is still in flight.
  const lastName = useRef(radio.name);

  useEffect(() => {
    lastName.current = radio.name;
    setName(radio.name);
  }, [radio.name]);

  // Dismissal discards the draft. Moving focus to the gear blurs a focused name
  // input synchronously, and that blur would otherwise commit the half-typed
  // name; the flag is set only around that one focus() call.
  const discarding = useRef(false);

  function close() {
    discarding.current = true;
    setName(lastName.current);
    setOpen(false);
    gearRef.current?.focus();
    discarding.current = false;
  }

  useLayoutEffect(() => {
    if (!open) {
      setPos(null);
      return;
    }
    lastName.current = radio.name;
    setName(radio.name); // never show an abandoned draft
    const g = gearRef.current?.getBoundingClientRect();
    if (!g) return;
    const h = panelRef.current?.offsetHeight || ESTIMATED_HEIGHT;
    const vw = window.innerWidth;
    const vh = window.innerHeight;
    const left = Math.max(MARGIN, Math.min(g.right - WIDTH, vw - WIDTH - MARGIN));
    let top = g.bottom + GAP;
    if (top + h > vh - MARGIN) top = Math.max(MARGIN, g.top - GAP - h); // flip above
    setPos({ left, top });
  }, [open]);

  useEffect(() => {
    if (open && pos) panelRef.current?.focus();
  }, [open, pos]);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: globalThis.PointerEvent) => {
      const t = e.target as Node | null;
      if (t && (panelRef.current?.contains(t) || gearRef.current?.contains(t))) return;
      close();
    };
    const onScroll = (e: Event) => {
      // Scrolling the drawer's own input (a long name) is not a reason to close.
      if (e.target instanceof Node && panelRef.current?.contains(e.target)) return;
      close();
    };
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (e.key === "Escape") close();
    };
    document.addEventListener("pointerdown", onDown);
    document.addEventListener("keydown", onKey);
    window.addEventListener("scroll", onScroll, true);
    window.addEventListener("resize", close);
    return () => {
      document.removeEventListener("pointerdown", onDown);
      document.removeEventListener("keydown", onKey);
      window.removeEventListener("scroll", onScroll, true);
      window.removeEventListener("resize", close);
    };
  }, [open]);

  function commitName() {
    if (discarding.current) return;
    const next = nextRadioName(name, lastName.current);
    if (next) {
      lastName.current = next;
      setName(next);
      onCommit({ ...radio, name: next });
    } else {
      setName(lastName.current);
    }
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
        className="radio-gear"
        data-open={open}
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
            onDragStart={stop}
            onDragOver={stop}
            onDrop={stop}
          >
            <div id={titleId} className="cap" style={{ color: "var(--tx-1)", fontSize: 10, letterSpacing: "0.14em" }}>
              Radio settings
            </div>
            <Row label="Name">
              <input
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
            <Row label="Variant">
              <select
                className="input"
                aria-label="Variant"
                style={{ height: 20, fontSize: 12, padding: "0 6px", width: 120 }}
                value={variantId}
                onChange={(e) => {
                  close();
                  onVariantChange(e.target.value);
                }}
              >
                {VARIANTS.map((v) => (
                  <option key={v.id} value={v.id}>
                    {v.label}
                  </option>
                ))}
              </select>
            </Row>
          </div>,
          document.body,
        )}
    </>
  );
}
