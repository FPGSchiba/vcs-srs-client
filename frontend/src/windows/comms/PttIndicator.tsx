import { Icon } from "../../shared/components/Icon";

interface Props {
  w: number | "fill";
  h: number;
  transmitting: boolean;
  /** The variant has room for the word as well as the icon. */
  showLabel: boolean;
}

const TITLE = "Push-to-talk is driven by your configured keybind, not this button";

/**
 * A live transmit INDICATOR, not a click-to-talk control — wiring a real
 * handler needs backend binding surface that does not exist. It stays a native
 * disabled <button>: one never dispatches `click`, so it cannot re-select the
 * card, it still restyles and re-texts as transmit state changes, and it gets
 * the browser's own "this does nothing" affordance.
 *
 * The visible text is deliberately terse ("PTT" / "TX", as
 * design/vcs/radio-variants.md §2 draws it): the box is 96x28 at VERTICAL, so
 * after `.ptt`'s 12px side padding, the 12px icon and the 6px gap only 54px
 * remain, and "PUSH-TO-TALK" (~103px at 11px mono + 0.18em tracking) wrapped.
 * The accessible name (aria-label) stays the full phrase.
 *
 * Its box comes from the variant descriptor, so the narrow variants shrink it
 * to an icon without needing a second component.
 */
export function PttIndicator({ w, h, transmitting, showLabel }: Readonly<Props>) {
  return (
    <button
      className={`ptt ${transmitting ? "keyed" : ""}`.trim()}
      type="button"
      aria-label={transmitting ? "transmitting" : "push-to-talk"}
      style={{
        width: w === "fill" ? "100%" : w,
        height: h,
        flexShrink: 0,
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        gap: 6,
      }}
      title={TITLE}
      disabled
    >
      <Icon name="mic" size={showLabel ? 12 : 14} />
      {showLabel && (transmitting ? "TX" : "PTT")}
    </button>
  );
}
