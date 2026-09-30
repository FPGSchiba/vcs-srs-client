import { useState, type KeyboardEvent, type MouseEvent, type WheelEvent } from "react";
import { MAX_KHZ, clampKhz, digitsOf, stepDigit } from "../freq";

interface LcdFreqProps {
  /** The canonical integer-kHz value. See shared/freq.ts. */
  khz: number;
  /** Digit size in px, supplied by the variant descriptor. */
  digitPx?: number;
  /** Render the "MHZ" suffix. The narrow variants drop it. */
  unit?: boolean;
  className?: string;
  /**
   * Opt-in editing. Omitted, the LCD is a read-only display with no focus stop
   * and no buttons. Supplied, it reports a new integer kHz value.
   */
  onChange?: (khz: number) => void;
}

/** Where the digit cursor starts: the 1 kHz decade. */
const INITIAL_PLACE = 0;

/**
 * LcdFreq renders an LCD-style frequency readout and, when editable, lets each
 * decade be changed on its own: hover a digit and wheel, or click it (shift to
 * go down). Keyboard users get ONE tab stop with spinbutton semantics —
 * ArrowLeft/ArrowRight move a digit cursor, ArrowUp/ArrowDown step the digit
 * under it — rather than a tab stop per digit, which would put seven stops on
 * every radio card.
 *
 * Digits are real <button>s, so click and Enter/Space are native behaviour and
 * typescript:S1082 does not apply. They carry tabIndex={-1} so only the LCD
 * itself is in the tab order.
 *
 * Typed entry is kept from the previous revision: digit keys build a draft,
 * Enter commits, Escape and blur discard. Blur is not a purposeful commit
 * gesture, and leaving a draft open would shadow a live server echo of the
 * value. While a draft is open the per-digit handlers are INERT — a partially
 * typed string has no stable decades to target — and the whole-LCD wheel keeps
 * its ±1 kHz fallback.
 *
 * Every value it emits is an integer kHz inside the 24-bit wire range; the MHz
 * float the DTO carries is derived by the caller at the boundary.
 */
export function LcdFreq({ khz, digitPx, unit, className, onChange }: Readonly<LcdFreqProps>) {
  const [draft, setDraft] = useState<string | null>(null);
  const [place, setPlace] = useState(INITIAL_PLACE);

  const editable = Boolean(onChange);
  // A decade's place is its stable identity; the "." and typed-draft characters
  // have none, so they fall back to their position (the draft is a transient
  // string with no per-character identity to preserve).
  const cells = (
    draft === null
      ? digitsOf(khz)
      : draft.split("").map((c) => ({ char: c, place: null as number | null }))
  ).map((c, pos) => ({ ...c, key: c.place === null ? `x${pos}` : `d${c.place}` }));
  const places = cells.filter((c) => c.place !== null).map((c) => c.place as number);

  function step(p: number, dir: 1 | -1) {
    if (!onChange || draft !== null) return;
    const next = stepDigit(khz, p, dir);
    setPlace(p);
    if (next !== clampKhz(khz)) onChange(next);
  }

  function onDigitWheel(e: WheelEvent<HTMLButtonElement>, p: number) {
    if (!editable || draft !== null) return;
    e.preventDefault();
    e.stopPropagation(); // do not also run the whole-LCD fallback
    step(p, e.deltaY < 0 ? 1 : -1);
  }

  function onDigitClick(e: MouseEvent<HTMLButtonElement>, p: number) {
    if (!editable) return;
    e.stopPropagation(); // a digit click must not select the card
    step(p, e.shiftKey ? -1 : 1);
  }

  function moveCursor(dir: -1 | 1) {
    // `places` runs most-significant first, so ArrowLeft (dir -1) moves toward
    // the LARGER decade, i.e. one index earlier. The separator is not in
    // `places` at all, so it is skipped for free.
    const i = places.indexOf(place);
    const from = i < 0 ? places.length - 1 : i;
    const next = places[Math.min(places.length - 1, Math.max(0, from + dir))];
    if (next !== undefined) setPlace(next);
  }

  function commitDraft() {
    if (draft === null) return;
    const value = draft === "" ? 0 : Number.parseInt(draft, 10);
    onChange?.(clampKhz(value));
    setDraft(null);
  }

  function onKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    if (!editable) return;
    if (/^[0-9]$/.test(e.key)) {
      e.preventDefault();
      setDraft((prev) => (prev ?? "") + e.key);
      return;
    }
    switch (e.key) {
      case "Backspace":
        e.preventDefault();
        setDraft((prev) => (prev ? prev.slice(0, -1) : prev));
        return;
      case "Enter":
        e.preventDefault();
        commitDraft();
        return;
      case "Escape":
        e.preventDefault();
        setDraft(null);
        return;
      case "ArrowLeft":
        e.preventDefault();
        moveCursor(-1);
        return;
      case "ArrowRight":
        e.preventDefault();
        moveCursor(1);
        return;
      case "ArrowUp":
        e.preventDefault();
        step(place, 1);
        return;
      case "ArrowDown":
        e.preventDefault();
        step(place, -1);
    }
  }

  /** The pre-existing whole-LCD fallback, live only while a draft is open. */
  function onLcdWheel(e: WheelEvent<HTMLDivElement>) {
    if (!editable || draft === null) return;
    e.preventDefault();
    const base = Number.parseInt(draft || "0", 10);
    onChange?.(clampKhz(base + (e.deltaY < 0 ? 1 : -1)));
    setDraft(null);
  }

  return (
    <div
      className={`lcd-screen ${className ?? ""}`.trim()}
      tabIndex={editable ? 0 : undefined}
      role={editable ? "spinbutton" : undefined}
      aria-label={editable ? "frequency" : undefined}
      aria-valuenow={editable ? khz : undefined}
      aria-valuemin={editable ? 0 : undefined}
      aria-valuemax={editable ? MAX_KHZ : undefined}
      onKeyDown={onKeyDown}
      onBlur={() => setDraft(null)}
      onWheel={onLcdWheel}
    >
      <fieldset
        className="lcd-digits"
        aria-label="frequency"
        style={digitPx ? { fontSize: digitPx, lineHeight: 1 } : undefined}
      >
        {cells.map((c) =>
          editable && c.place !== null ? (
            <button
              key={c.key}
              type="button"
              tabIndex={-1}
              className="lcd-digit"
              data-cursor={c.place === place}
              aria-label={`digit ${c.place}`}
              onMouseDown={(e) => e.preventDefault()} // never steal focus from the LCD
              onWheel={(e) => onDigitWheel(e, c.place as number)}
              onClick={(e) => onDigitClick(e, c.place as number)}
            >
              {c.char}
            </button>
          ) : (
            <span key={c.key} className="lcd-digit">
              {c.char}
            </span>
          ),
        )}
        {unit && <span className="lcd-unit">MHZ</span>}
      </fieldset>
    </div>
  );
}
