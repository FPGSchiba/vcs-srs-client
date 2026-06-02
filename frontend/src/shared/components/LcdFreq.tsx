interface LcdFreqProps {
  value: number;
  className?: string;
}

/**
 * LcdFreq renders a read-only LCD-style frequency display, ported from the
 * prototype's `radio.jsx`/`atoms.jsx` LcdFreq look. The frequency is formatted
 * to 3 decimals (e.g. `118.500`) and rendered as per-character monospace digits
 * inside the `.lcd-screen` wrapper so the ported CSS applies unchanged. Unlike
 * the prototype this is read-only (no drag/wheel-to-edit). Presentational only.
 */
export function LcdFreq({ value, className }: LcdFreqProps) {
  const str = value.toFixed(3); // e.g. "118.500"
  const digits = str.split("");

  return (
    <div className={`lcd-screen ${className ?? ""}`.trim()}>
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
