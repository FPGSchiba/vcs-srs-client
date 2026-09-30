import type { KeyboardEvent, MouseEvent, ReactNode } from "react";

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
 * not show an intercom toggle without losing the information.
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
 * Every kind of interactive descendant a click must NOT turn into a card
 * selection. INVARIANT: this must list every interactive element kind that can
 * appear inside a card; a kind missing here silently selects the card behind
 * its control, with no failing test unless one is added for it.
 */
const CONTROLS =
  "button,input,select,textarea,a[href],[tabindex],[contenteditable]," +
  "[role=button],[role=checkbox],[role=spinbutton],[role=switch]";

/**
 * The frame's border colour. Precedence: a disabled radio is muted (dimmer blue
 * when selected, --bd-1 otherwise); an enabled one is blue when selected or
 * transmitting, --bd-2 otherwise.
 */
function frameBorder({
  selected,
  transmitting,
  disabled,
}: Readonly<{ selected: boolean; transmitting: boolean; disabled: boolean }>): string {
  if (disabled) {
    return selected
      ? "color-mix(in srgb, var(--ac-primary) 45%, var(--bg-1))"
      : "var(--bd-1)";
  }
  return selected || transmitting ? "var(--ac-primary)" : "var(--bd-2)";
}

/**
 * RadioFrame is the card's border, corner brackets, intercom edge, disabled
 * hatch and selection behaviour. Every variant renders inside one, so the five
 * states read identically at 150px and at 360px — see
 * design/vcs/radio-variants.md §4.
 *
 * It is the card's selection stop (its controls — name, LCD, settings gear — are separate
 * tab stops). It is a labelled role="group" inside the grid's <ul>/<li> list,
 * with the selected card marked aria-current — NOT a listbox option: an
 * option's content must be presentational, and a card holds several focusable
 * controls, so the listbox/option framing was invalid ARIA (and made the
 * variant <select>'s native options collide with the cards in queries).
 *
 * The frame itself never owns the controls' events: a key bubbled from a child
 * control is ignored (typing a space in the name does not re-select), and so
 * is a click that lands on — or inside — an interactive descendant (the LCD,
 * a digit, a chip, the gear). The one exception is the name label, whose
 * single click still selects the card; only its double-click renames.
 * A click bubbling through a React portal (the settings panel) comes from
 * outside this element's DOM and is ignored too.
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
}: Readonly<Props>) {
  const accent = frameAccent({ selected, receiving, transmitting, intercom, disabled });
  // A disabled radio must stay selectable, so selection has to stay visible:
  // aria-current is invisible to sighted users, and with two or more disabled
  // radios nothing else says which one is selected. Keep the blue, muted into
  // the background. The brackets still drain to --bd-1 via frameAccent.
  // (Clicking a disabled radio only selects it; the ON toggle re-enables it.)
  const border = frameBorder({ selected, transmitting, disabled });

  function onKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    if (e.target !== e.currentTarget) return;
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      onSelect();
    }
  }

  function onClick(e: MouseEvent<HTMLDivElement>) {
    const target = e.target as Element;
    if (!e.currentTarget.contains(target)) return; // portalled panel
    // Bounded to this frame: an interactive ancestor OUTSIDE the card must not
    // suppress selection. The frame itself matches [tabindex], so it is
    // excluded by identity.
    const control = target.closest(CONTROLS);
    if (
      control &&
      control !== e.currentTarget &&
      e.currentTarget.contains(control) &&
      !control.hasAttribute("data-card-select")
    )
      return;
    onSelect();
  }

  return (
    <div
      className="radio"
      role="group"
      aria-label={label}
      aria-current={selected ? "true" : undefined}
      tabIndex={0}
      data-selected={selected}
      data-rx={receiving}
      data-tx={transmitting}
      data-intercom={intercom}
      data-disabled={disabled}
      onClick={onClick}
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
