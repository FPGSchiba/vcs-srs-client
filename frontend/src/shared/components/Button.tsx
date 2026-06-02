import type { ButtonHTMLAttributes } from "react";

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: "primary" | "ghost" | "danger";
  size?: "sm" | "lg";
  icon?: boolean;
}

/**
 * Button maps semantic props to the design's `btn` classNames, ported from the
 * prototype. variant -> `btn-primary`/`btn-ghost`/`btn-danger`, size ->
 * `btn-sm`/`btn-lg`, icon -> `btn-icon`. Any incoming `className` is merged and
 * all other native button attributes (onClick, disabled, type, title, children)
 * are spread through. Presentational only.
 */
export function Button({
  variant,
  size,
  icon,
  className,
  ...rest
}: ButtonProps) {
  const classes = [
    "btn",
    variant && `btn-${variant}`,
    size && `btn-${size}`,
    icon && "btn-icon",
    className,
  ]
    .filter(Boolean)
    .join(" ");

  return <button className={classes} {...rest} />;
}
