import { useState, type KeyboardEvent, type WheelEvent } from "react";

interface LcdFreqProps {
  value: number;
  className?: string;
  /**
   * Opt-in editing. When omitted the component renders exactly as before
   * (Phase 1 behaviour): a presentational, read-only display. When provided,
   * the LCD becomes focusable and accepts typed digits (Enter to commit,
   * Escape to revert) and wheel steps, and calls back with the committed
   * value.
   */
  onChange?: (value: number) => void;
}

// The wire field is a 24-bit unsigned kHz integer (see internal/voice/freq.go
// KHz) -- this is the client's ONLY canonical frequency representation.
// Editing therefore always operates on an integer kHz value, never on the
// displayed MHz float, so repeated small edits cannot accumulate
// floating-point drift.
const MIN_KHZ = 0;
const MAX_KHZ = 16_777_215; // 2^24 - 1

function clampKhz(khz: number): number {
  return Math.min(MAX_KHZ, Math.max(MIN_KHZ, khz));
}

function mhzToKhz(mhz: number): number {
  return Math.round(mhz * 1000);
}

// Mirrors internal/voice/freq.go's KHz.MHz32 EXACTLY
// (`float32(uint32(k)) / 1000.0`): the server compares the value we
// advertise against this same expression with ==, so any other rounding
// here would silently desync from the server's derived value.
function khzToMhz(khz: number): number {
  return Math.fround(khz / 1000);
}

/**
 * LcdFreq renders an LCD-style frequency display, ported from the
 * prototype's `radio.jsx`/`atoms.jsx` LcdFreq look. The frequency is
 * formatted to 3 decimals (e.g. `118.500`) and rendered as per-character
 * monospace digits inside the `.lcd-screen` wrapper so the ported CSS
 * applies unchanged.
 *
 * Editing (opt-in via `onChange`) types directly in kHz integers: digit
 * keys build up a draft kHz value, Enter commits it (converted to the MHz
 * float the wire DTO carries, via the identical expression the backend
 * uses), Escape reverts the draft, and wheel steps the committed value by
 * 1 kHz. The draft is clamped to the 24-bit wire range before it is ever
 * handed to `onChange`.
 */
export function LcdFreq({ value, className, onChange }: LcdFreqProps) {
  const [draft, setDraft] = useState<string | null>(null);

  const committedStr = value.toFixed(3); // e.g. "118.500"
  const digits = (draft ?? committedStr).split("");

  function commitDraft() {
    if (draft === null) return;
    const khz = draft === "" ? 0 : Number.parseInt(draft, 10);
    onChange?.(khzToMhz(clampKhz(khz)));
    setDraft(null);
  }

  function handleKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    if (!onChange) return;
    if (/^[0-9]$/.test(e.key)) {
      e.preventDefault();
      setDraft((prev) => (prev ?? "") + e.key);
      return;
    }
    if (e.key === "Backspace") {
      e.preventDefault();
      setDraft((prev) => (prev ? prev.slice(0, -1) : prev));
      return;
    }
    if (e.key === "Enter") {
      e.preventDefault();
      commitDraft();
      return;
    }
    if (e.key === "Escape") {
      e.preventDefault();
      setDraft(null);
    }
  }

  function handleWheel(e: WheelEvent<HTMLDivElement>) {
    if (!onChange) return;
    e.preventDefault();
    const base = draft !== null ? Number.parseInt(draft || "0", 10) : mhzToKhz(value);
    const next = clampKhz(base + (e.deltaY < 0 ? 1 : -1));
    onChange(khzToMhz(next));
    setDraft(null);
  }

  return (
    <div
      className={`lcd-screen ${className ?? ""}`.trim()}
      tabIndex={onChange ? 0 : undefined}
      role={onChange ? "spinbutton" : undefined}
      aria-label={onChange ? "frequency" : undefined}
      onKeyDown={handleKeyDown}
      onWheel={handleWheel}
    >
      <span className="lcd-digits">
        {digits.map((c, i) => (
          <span key={i} className="lcd-digit">
            {c}
          </span>
        ))}
      </span>
    </div>
  );
}
