interface ToggleProps {
  on: boolean;
  onChange: (v: boolean) => void;
  lg?: boolean;
  "aria-label"?: string;
}

/**
 * Toggle is a controlled switch ported from the design prototype's `atoms.jsx`
 * Toggle. It renders a `role="switch"` button reflecting `on` via `aria-checked`
 * and the `on`/`lg` classNames so the ported CSS applies unchanged. Clicking
 * invokes `onChange` with the negated value. Presentational only.
 */
export function Toggle({
  on,
  onChange,
  lg,
  "aria-label": ariaLabel,
}: ToggleProps) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={on}
      aria-label={ariaLabel}
      className={`toggle ${on ? "on" : ""} ${lg ? "lg" : ""}`}
      onClick={() => onChange(!on)}
    />
  );
}
