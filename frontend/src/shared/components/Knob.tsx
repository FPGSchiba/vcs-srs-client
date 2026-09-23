import { useMemo } from "react";
import type { CSSProperties, KeyboardEvent, PointerEvent } from "react";

interface KnobProps {
  /** Current value, in whatever scale `min`/`max` describe -- the design's
   * knobs default to a 0–100 scale. */
  value: number;
  onChange: (v: number) => void;
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
 */
export function Knob({
  value,
  onChange,
  label,
  size = 44,
  min = 0,
  max = 100,
  ticks = 9,
  center = false,
  className,
}: KnobProps) {
  const range = max - min;
  const t = (value - min) / range;
  const angle = -135 + t * 270;

  const clamp = (v: number) => Math.max(min, Math.min(max, v));

  const onPointerDown = (e: PointerEvent<HTMLDivElement>) => {
    e.preventDefault();
    const startY = e.clientY;
    const startVal = value;
    const move = (ev: globalThis.PointerEvent) => {
      const dy = startY - ev.clientY;
      onChange(clamp(startVal + (dy / 100) * range));
    };
    const up = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  };

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const fine = range * FINE_STEP_FRACTION;
    const coarse = range * COARSE_STEP_FRACTION;
    switch (e.key) {
      case "ArrowUp":
      case "ArrowRight":
        e.preventDefault();
        onChange(clamp(value + fine));
        break;
      case "ArrowDown":
      case "ArrowLeft":
        e.preventDefault();
        onChange(clamp(value - fine));
        break;
      case "PageUp":
        e.preventDefault();
        onChange(clamp(value + coarse));
        break;
      case "PageDown":
        e.preventDefault();
        onChange(clamp(value - coarse));
        break;
      case "Home":
        e.preventDefault();
        onChange(min);
        break;
      case "End":
        e.preventDefault();
        onChange(max);
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
      aria-valuenow={value}
      aria-valuemin={min}
      aria-valuemax={max}
      aria-label={label}
      onPointerDown={onPointerDown}
      onKeyDown={onKeyDown}
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
