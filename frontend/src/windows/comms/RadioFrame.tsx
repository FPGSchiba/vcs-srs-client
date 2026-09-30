import type { KeyboardEvent, ReactNode } from "react";

export interface RadioState {
  selected: boolean;
  receiving: boolean;
  transmitting: boolean;
  intercom: boolean;
  /** The radio's enabled flag is off. NOT "this card is inert". */
  disabled: boolean;
}

interface Props extends RadioState {
  w: number;
  h: number;
  /** The card's accessible name — "R01 Fleet Common". */
  label: string;
  onSelect: () => void;
  children: ReactNode;
}

/**
 * The accent the corner brackets take.
 *
 * Intercom wins over selection deliberately: the two use DIFFERENT colour
 * channels (amber vs blue) so a selected intercom radio shows both — the blue
 * border from `selected`, the amber edge and brackets from `intercom` —
 * instead of one state hiding the other. This is what lets the narrow variants
 * drop the ICOM chip without losing the information.
 *
 * Disabled drains everything: a radio that is off should not compete for
 * attention with one that is live.
 */
export function frameAccent(s: RadioState): string {
  if (s.disabled) return "var(--bd-1)";
  if (s.intercom) return "var(--ac-warn)";
  if (s.transmitting || s.selected) return "var(--ac-primary)";
  return "var(--bd-2)";
}

const BRACKET = 10;

const CORNERS = [
  { k: "tl", o: 0.7, left: 0, top: 0, bl: true, bt: true },
  { k: "tr", o: 0.5, right: 0, top: 0, br: true, bt: true },
  { k: "bl", o: 0.5, left: 0, bottom: 0, bl: true, bb: true },
] as const;

/**
 * RadioFrame is the card's border, corner brackets, intercom edge, disabled
 * hatch and selection behaviour. Every variant renders inside one, so the five
 * states read identically at 150px and at 360px — see
 * design/vcs/radio-variants.md §4.
 *
 * It is the card's selection stop (its controls — name, LCD, chips — are separate
 * tab stops) and carries role="option" against the
 * grid's role="listbox". A key bubbled from a child control (the name input,
 * the LCD) is ignored, so typing a space in the name does not re-select.
 */
export function RadioFrame({
  w,
  h,
  label,
  onSelect,
  children,
  selected,
  receiving,
  transmitting,
  intercom,
  disabled,
}: Props) {
  const accent = frameAccent({ selected, receiving, transmitting, intercom, disabled });
  // A disabled radio must stay selectable (clicking it is how it is turned back
  // on), so selection has to stay visible: keep the blue, muted into the
  // background. The brackets still drain to --bd-1 via frameAccent.
  const border = disabled
    ? selected
      ? "color-mix(in srgb, var(--ac-primary) 45%, var(--bg-1))"
      : "var(--bd-1)"
    : selected || transmitting
      ? "var(--ac-primary)"
      : "var(--bd-2)";

  function onKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    if (e.target !== e.currentTarget) return;
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      onSelect();
    }
  }

  return (
    <div
      className="radio"
      role="option"
      aria-label={label}
      aria-selected={selected}
      tabIndex={0}
      data-selected={selected}
      data-rx={receiving}
      data-tx={transmitting}
      data-intercom={intercom}
      data-disabled={disabled}
      onClick={onSelect}
      onKeyDown={onKeyDown}
      style={{
        position: "relative",
        width: w,
        height: h,
        boxSizing: "border-box",
        overflow: "hidden",
        border: `1px solid ${border}`,
        background: disabled
          ? "var(--bg-1)"
          : "linear-gradient(180deg, var(--bg-3), var(--bg-2))",
        boxShadow: transmitting
          ? "0 0 0 1px var(--ac-primary), 0 0 14px -4px var(--ac-primary)"
          : undefined,
        opacity: disabled ? 0.72 : 1,
        cursor: "pointer",
      }}
    >
      {intercom && (
        <span
          data-intercom-edge
          aria-hidden="true"
          style={{
            position: "absolute",
            left: 0,
            top: 0,
            bottom: 0,
            width: 3,
            background: "var(--ac-warn)",
            pointerEvents: "none",
          }}
        />
      )}
      {disabled && (
        <span
          data-disabled-hatch
          aria-hidden="true"
          style={{
            position: "absolute",
            inset: 0,
            pointerEvents: "none",
            backgroundImage:
              "repeating-linear-gradient(135deg, transparent 0 7px, color-mix(in srgb, var(--bd-1) 55%, transparent) 7px 8px)",
          }}
        />
      )}
      {CORNERS.map((c) => (
        <span
          key={c.k}
          data-bracket={c.k}
          aria-hidden="true"
          style={{
            position: "absolute",
            width: BRACKET,
            height: BRACKET,
            pointerEvents: "none",
            opacity: c.o,
            left: "left" in c ? c.left : undefined,
            right: "right" in c ? c.right : undefined,
            top: "top" in c ? c.top : undefined,
            bottom: "bottom" in c ? c.bottom : undefined,
            borderLeft: "bl" in c ? `1px solid ${accent}` : undefined,
            borderRight: "br" in c ? `1px solid ${accent}` : undefined,
            borderTop: "bt" in c ? `1px solid ${accent}` : undefined,
            borderBottom: "bb" in c ? `1px solid ${accent}` : undefined,
          }}
        />
      ))}
      {children}
    </div>
  );
}
