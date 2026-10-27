import { useEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, KeyboardEvent, PointerEvent } from "react";

interface KnobProps {
  /** Current value, in whatever scale `min`/`max` describe -- the design's
   * knobs default to a 0–100 scale. */
  value: number;
  onChange: (v: number) => void;
  /** Fires once when an adjustment ENDS: on pointerup / pointercancel after
   * a drag, and on keyup after a run of key presses (so keyboard
   * auto-repeat is one adjustment, not one per repeat). A parent that
   * throttles `onChange` uses this for the final commit. Optional: without
   * it the Knob behaves as before. */
  onChangeEnd?: (v: number) => void;
  /** Accessible name and the visible caption under the knob. */
  label?: string;
  size?: number;
  min?: number;
  max?: number;
  /** Number of tick marks drawn around the rim. */
  ticks?: number;
  /** Centered variant (e.g. a balance knob): ticks light outward from the
   * middle of the range instead of accumulating from the minimum. */
  center?: boolean;
  className?: string;
}

// Arrow keys move 1% of the range per press, Page keys move 10% -- this
// reproduces the brief's "±1, ±10" fine/coarse steps for the design's
// default 0–100 scale while still scaling sensibly for a knob configured
// with a different min/max (e.g. the centered -50..50 balance variant).
const FINE_STEP_FRACTION = 1 / 100;
const COARSE_STEP_FRACTION = 1 / 10;
const KNOB_KEYS = new Set([
  "ArrowUp", "ArrowRight", "ArrowDown", "ArrowLeft", "PageUp", "PageDown", "Home", "End",
]);

/**
 * Knob renders a rotary control ported from the design prototype's
 * `atoms.jsx` Knob: a `.knob` container holding rim tick marks, a
 * `.knob-body` with a rotating `.knob-tick` indicator, and an optional
 * `.knob-label` caption -- classNames, structure and the angle math
 * (-135deg at min to +135deg at max) all match the prototype so the ported
 * CSS applies unchanged.
 *
 * Unlike the prototype (a bare pointer-draggable div, unusable by keyboard
 * and invisible to assistive tech), this carries `role="slider"` with
 * `aria-valuenow`/`aria-valuemin`/`aria-valuemax` and an accessible name
 * from `label`, is focusable, and supports keyboard operation: Arrow keys
 * for fine (±1%) adjustment, Page keys for coarse (±10%) adjustment, and
 * Home/End to jump to the extremes. The prototype's pointer-drag behaviour
 * (vertical drag, 100px of travel spans the full range) is kept on top of
 * that, not replaced by it.
 *
 * The drag is effect-scoped (Phase 6.9 CL3, FE15): the window listeners are
 * owned by an effect keyed on the drag, so they are removed on pointerup,
 * on pointercancel and on unmount alike -- a knob unmounted mid-drag (VOX
 * toggled off under it, say) can no longer keep calling `onChange`. While
 * dragging, the Knob renders its own local draft, so it follows the pointer
 * even when the parent commits on a throttle.
 */
export function Knob({
  value,
  onChange,
  onChangeEnd,
  label,
  size = 44,
  min = 0,
  max = 100,
  ticks = 9,
  center = false,
  className,
}: KnobProps) {
  // The value shown while an adjustment is in progress; null otherwise.
  const [draft, setDraft] = useState<number | null>(null);
  const range = max - min;
  const shown = draft ?? value;
  const t = (shown - min) / range;
  const angle = -135 + t * 270;

  const clamp = (v: number) => Math.max(min, Math.min(max, v));

  const draftRef = useRef<number | null>(null);
  const [drag, setDrag] = useState<{ startY: number; startVal: number } | null>(null);
  // The latest props for the drag effect, without re-subscribing per render.
  const latest = useRef({ onChange, onChangeEnd, min, max });
  latest.current = { onChange, onChangeEnd, min, max };

  useEffect(() => {
    if (!drag) return;
    const move = (ev: globalThis.PointerEvent) => {
      const { min: lo, max: hi } = latest.current;
      // Vertical drag: 100px of travel spans the full range.
      const raw = drag.startVal + ((drag.startY - ev.clientY) / 100) * (hi - lo);
      const v = Math.max(lo, Math.min(hi, raw));
      draftRef.current = v;
      setDraft(v);
      latest.current.onChange(v);
    };
    const end = () => {
      const v = draftRef.current;
      draftRef.current = null;
      setDraft(null);
      setDrag(null);
      if (v !== null) latest.current.onChangeEnd?.(v);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", end);
    window.addEventListener("pointercancel", end);
    return () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", end);
      window.removeEventListener("pointercancel", end);
    };
  }, [drag]);

  const onPointerDown = (e: PointerEvent<HTMLDivElement>) => {
    e.preventDefault();
    try {
      e.currentTarget.setPointerCapture?.(e.pointerId);
    } catch {
      /* capture is a nicety; the window listeners still deliver the drag */
    }
    draftRef.current = null;
    setDrag({ startY: e.clientY, startVal: value });
  };

  // Keyboard: each keydown calls onChange as before. With onChangeEnd, a run
  // of presses steps from the Knob's own draft (so auto-repeat keeps
  // climbing while a throttled parent lags) and ends on keyup or blur.
  const keyStep = (next: number) => {
    onChange(next);
    if (onChangeEnd && !drag) {
      draftRef.current = next;
      setDraft(next);
    }
  };
  const endKeys = () => {
    if (drag) return;
    const v = draftRef.current;
    if (v === null) return;
    draftRef.current = null;
    setDraft(null);
    onChangeEnd?.(v);
  };
  const onKeyUp = (e: KeyboardEvent<HTMLDivElement>) => {
    if (KNOB_KEYS.has(e.key)) endKeys();
  };

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const fine = range * FINE_STEP_FRACTION;
    const coarse = range * COARSE_STEP_FRACTION;
    const base = onChangeEnd && draftRef.current !== null && !drag ? draftRef.current : value;
    switch (e.key) {
      case "ArrowUp":
      case "ArrowRight":
        e.preventDefault();
        keyStep(clamp(base + fine));
        break;
      case "ArrowDown":
      case "ArrowLeft":
        e.preventDefault();
        keyStep(clamp(base - fine));
        break;
      case "PageUp":
        e.preventDefault();
        keyStep(clamp(base + coarse));
        break;
      case "PageDown":
        e.preventDefault();
        keyStep(clamp(base - coarse));
        break;
      case "Home":
        e.preventDefault();
        keyStep(min);
        break;
      case "End":
        e.preventDefault();
        keyStep(max);
        break;
      default:
        break;
    }
  };

  const tickEls = useMemo(() => {
    const arr = [];
    for (let i = 0; i < ticks; i++) {
      const a = -135 + (i / (ticks - 1)) * 270;
      const lit = center
        ? (a <= angle && a >= 0) || (a >= angle && a <= 0)
        : a <= angle;
      arr.push(
        <span
          key={i}
          style={{
            position: "absolute",
            left: "50%",
            top: "50%",
            width: 1,
            height: size * 0.06,
            background: lit ? "var(--ac-primary)" : "var(--bd-2)",
            transform: `translate(-50%, -${size / 2 + 4}px) rotate(${a}deg)`,
            transformOrigin: `50% ${size / 2 + 4}px`,
            boxShadow: lit ? "0 0 4px var(--ac-primary)" : "none",
          }}
        />,
      );
    }
    return arr;
  }, [angle, ticks, size, center]);

  return (
    <div
      className={`knob ${className ?? ""}`.trim()}
      style={{ "--size": `${size}px` } as CSSProperties}
      role="slider"
      tabIndex={0}
      aria-valuenow={shown}
      aria-valuemin={min}
      aria-valuemax={max}
      aria-label={label}
      onPointerDown={onPointerDown}
      onKeyDown={onKeyDown}
      onKeyUp={onKeyUp}
      onBlur={endKeys}
    >
      {tickEls}
      <div className="knob-body">
        <span
          className="knob-tick"
          style={{
            height: size * 0.32,
            transform: `translateX(-50%) rotate(${angle}deg)`,
            transformOrigin: `50% ${size / 2 - 4}px`,
            top: 4,
          }}
        />
      </div>
      {label && <div className="knob-label">{label}</div>}
    </div>
  );
}
