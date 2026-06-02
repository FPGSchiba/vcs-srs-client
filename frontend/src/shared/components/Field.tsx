import type { CSSProperties, ReactNode } from "react";

interface FieldProps {
  label: string;
  htmlFor?: string;
  children: ReactNode;
  style?: CSSProperties;
}

/**
 * Field wraps a labelled form control in the design's `.field` layout. When
 * `htmlFor` is provided the label renders as a `<label>` bound to the control;
 * otherwise it renders as a `<span>`. classNames (`field`, `field-label`) match
 * the design so the ported CSS applies unchanged. Presentational only.
 */
export function Field({ label, htmlFor, children, style }: FieldProps) {
  return (
    <div className="field" style={style}>
      {htmlFor ? (
        <label htmlFor={htmlFor} className="field-label">
          {label}
        </label>
      ) : (
        <span className="field-label">{label}</span>
      )}
      {children}
    </div>
  );
}
