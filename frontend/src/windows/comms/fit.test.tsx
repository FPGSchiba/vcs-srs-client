import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { SHELLS, type RadioShellProps } from "./shells";
import { VARIANTS, type RadioVariant } from "./variants";

/**
 * jsdom has no layout, so this cannot measure. It is a model: the shell's own
 * padding and gaps are READ from its rendered inline styles (so shaving one
 * changes the result), and the leaf row heights below were measured in Chromium
 * against the real components (RadioCard at each variant, frame height auto).
 *
 * The invariant it guards: a card's content must fit inside `h` minus the
 * frame's 1px top and bottom border. Everything has flex-shrink 1, so an
 * overflow is absorbed silently and shows up only as clipped LCD digits.
 */
const FRAME_BORDER = 2;
const px = (s: string | undefined) => Number.parseFloat(s ?? "0") || 0;

/** LCD box height by digit size (measured): digits + padding + border + inline strut. */
const LCD_MEASURED: Record<number, number> = { 30: 44.19, 22: 37.19, 21: 36.19, 18: 34.19 };
const lcdHeight = (digitPx: number) => LCD_MEASURED[digitPx] ?? digitPx + 16.2; // conservative for a new size
const headerHeight = (v: RadioVariant) => (v.shows.chips === "none" ? 16.8 : 18); // 18 = the toggle
const STATUS_TEXT = 15.39;
const statusHeight = (v: RadioVariant) => STATUS_TEXT + (v.shows.statusDivider ? 7 : 0); // 6 pad + 1 border

function pieces(variant: RadioVariant): RadioShellProps {
  return {
    variant,
    rid: <span>R01</span>,
    name: <span>Fleet Common</span>,
    lcd: <span data-testid="lcd" />,
    talker: <span />,
    ptt: <span data-testid="ptt" />,
    chips: <span />,
  };
}

/** Required frame height, in px, for a variant rendered through its own shell. */
function requiredHeight(v: RadioVariant): number {
  const Shell = SHELLS[v.orientation];
  const { unmount } = render(<Shell {...pieces(v)} />);
  const outer = screen.getByTestId("shell-outer");
  const gap = px(outer.style.gap);
  const lcd = lcdHeight(v.lcdPx);
  const header = headerHeight(v);
  const status = statusHeight(v);
  let content: number;
  if (v.orientation === "column") {
    const rows = [header, lcd, v.ptt.h, status];
    content = rows.reduce((a, b) => a + b, 0) + gap * (rows.length - 1);
  } else if (v.density === "compact") {
    const textGap = px((outer.children[1] as HTMLElement).style.gap);
    content = Math.max(lcd, header + textGap + status, v.ptt.h);
  } else {
    const colGap = px((outer.children[0] as HTMLElement).style.gap);
    content = Math.max(header + lcd + status + colGap * 2, v.ptt.h);
  }
  const total = content + px(outer.style.paddingTop) + px(outer.style.paddingBottom) + FRAME_BORDER;
  unmount();
  return total;
}

describe("every variant's content fits its declared height", () => {
  for (const v of VARIANTS) {
    it(`${v.id}: rows + gaps + padding + border fit in h (${v.h})`, () => {
      expect(requiredHeight(v)).toBeLessThanOrEqual(v.h);
    });
  }
});
