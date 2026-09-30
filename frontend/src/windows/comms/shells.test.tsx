import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { SHELLS, type RadioShellProps } from "./shells";
import { variantById, VARIANTS } from "./variants";

function pieces(variant = variantById("vertical")): RadioShellProps {
  return {
    variant,
    rid: <span data-testid="rid">R01</span>,
    name: <span data-testid="name">Fleet Common</span>,
    lcd: <span data-testid="lcd">118.500</span>,
    talker: <span data-testid="talker">no traffic</span>,
    ptt: <span data-testid="ptt">PTT</span>,
    chips: <span data-testid="chips">ON</span>,
  };
}

describe("every variant renders through a shell", () => {
  it("has a shell for each orientation in the registry", () => {
    for (const v of VARIANTS) expect(SHELLS[v.orientation]).toBeTypeOf("function");
  });

  it("renders every piece it is handed, at every variant", () => {
    for (const v of VARIANTS) {
      const Shell = SHELLS[v.orientation];
      const { unmount } = render(<Shell {...pieces(v)} />);
      for (const id of ["rid", "name", "lcd", "talker", "ptt"]) {
        expect(screen.getByTestId(id), `${v.id} dropped ${id}`).toBeInTheDocument();
      }
      unmount();
    }
  });

  it("shows chips only where the descriptor asks for them", () => {
    for (const v of VARIANTS) {
      const Shell = SHELLS[v.orientation];
      const { unmount } = render(<Shell {...pieces(v)} />);
      if (v.shows.chips === "none") {
        expect(screen.queryByTestId("chips"), `${v.id} should hide chips`).toBeNull();
      } else {
        expect(screen.getByTestId("chips"), `${v.id} should show chips`).toBeInTheDocument();
      }
      unmount();
    }
  });
});

describe("the column shell", () => {
  it("draws the status divider exactly when shows.statusDivider says so", () => {
    // The SAME variant with only the field toggled, so nothing else can explain
    // the difference. toHaveStyle cannot see this (jsdom drops var() inside a
    // border shorthand), so read the inline style attribute.
    const Column = SHELLS.column;
    const vertical = variantById("vertical");
    const divider = "border-top: 1px solid var(--bd-1)";
    const styleOf = () => screen.getByTestId("shell-status").getAttribute("style") ?? "";

    const on = render(<Column {...pieces(vertical)} />);
    expect(styleOf()).toContain(divider);
    on.unmount();

    const off = render(
      <Column {...pieces({ ...vertical, shows: { ...vertical.shows, statusDivider: false } })} />,
    );
    expect(styleOf()).not.toContain("border-top");
    off.unmount();

    // Honoured in the compact branch too: the field is a whole contract.
    const compact = { ...vertical, density: "compact" as const };
    render(<Column {...pieces(compact)} />);
    expect(styleOf()).toContain(divider);
  });

  it("left-aligns the narrow-v name beside the rid rather than centring it", () => {
    const Column = SHELLS.column;
    render(<Column {...pieces(variantById("narrow-v"))} />);
    const header = screen.getByTestId("shell-header");
    // justify-content: flex-start is the flex default, so it is belt-and-braces
    // declared intent; what actually left-aligns is the DOM order below plus a
    // slot that neither centres its text nor takes auto margins.
    expect(header).toHaveStyle({ justifyContent: "flex-start" });
    const slot = screen.getByTestId("shell-name-slot");
    expect(slot.style.textAlign).not.toBe("center");
    expect(slot.style.margin).not.toContain("auto");
    expect(slot.style.marginLeft).not.toBe("auto");
    // rid first, then the name slot (which wraps the name).
    const kids = within(header).getAllByTestId(/^(rid|shell-name-slot)$/);
    expect(kids.map((k) => k.getAttribute("data-testid"))).toEqual(["rid", "shell-name-slot"]);
  });
});

describe("the row shell status divider", () => {
  it("draws the divider exactly when shows.statusDivider says so, in both branches", () => {
    const Row = SHELLS.row;
    const divider = "border-top: 1px solid var(--bd-1)";
    const styleOf = () => screen.getByTestId("shell-status").getAttribute("style") ?? "";
    // Synthetic ROW variants: no shipped row variant has statusDivider on.
    for (const base of ["horizontal", "narrow-h"]) {
      const v = variantById(base);
      const off = render(<Row {...pieces({ ...v, shows: { ...v.shows, statusDivider: false } })} />);
      expect(styleOf(), `${base} off`).not.toContain("border-top");
      off.unmount();
      const on = render(<Row {...pieces({ ...v, shows: { ...v.shows, statusDivider: true } })} />);
      expect(styleOf(), `${base} on`).toContain(divider);
      expect(styleOf(), `${base} on`).toContain("padding-top: 6px");
      on.unmount();
    }
  });
});

describe("the row shell", () => {
  it("puts the PTT beside the content column, not under it", () => {
    const Row = SHELLS.row;
    render(<Row {...pieces(variantById("horizontal"))} />);
    expect(screen.getByTestId("shell-outer")).toHaveStyle({
      flexDirection: "row",
      alignItems: "center",
    });
  });
});

// Review Focus #3: a name longer than the card must never push the LCD out.
describe("a name longer than the card", () => {
  const LONG = "Fleet Common Alpha Bravo Charlie Delta Echo Foxtrot Golf Hotel";

  it("truncates instead of growing, at every variant", () => {
    for (const v of VARIANTS) {
      const Shell = SHELLS[v.orientation];
      const p = pieces(v);
      const { unmount } = render(
        <Shell {...p} name={<span data-testid="name">{LONG}</span>} />,
      );
      const slot = screen.getByTestId("shell-name-slot");
      expect(slot, `${v.id} name slot must clip`).toHaveStyle({
        overflow: "hidden",
        minWidth: "0px",
      });
      // Its row must also allow the shrink, or the clip never takes effect.
      expect(screen.getByTestId("shell-header"), `${v.id} header must allow shrink`).toHaveStyle({
        minWidth: "0px",
      });
      // The LCD is still rendered and still a sibling, not pushed out.
      expect(screen.getByTestId("lcd"), `${v.id} lost its LCD`).toBeInTheDocument();
      unmount();
    }
  });
});

// The invariant made executable: a fifth variant is a descriptor and nothing
// else. These never touch a shell; if one fails, a shell has grown a proxy.
describe("a synthetic fifth variant", () => {
  const order = (a: string, b: string) =>
    !!(screen.getByTestId(a).compareDocumentPosition(screen.getByTestId(b)) &
      Node.DOCUMENT_POSITION_FOLLOWING);

  it("takes the compact column path from density alone, not lcdPx", () => {
    const Column = SHELLS.column;
    const v = { ...variantById("vertical"), density: "compact" as const, lcdPx: 30 };
    const { unmount } = render(<Column {...pieces(v)} />);
    // Compact: status line precedes the PTT.
    expect(order("talker", "ptt")).toBe(true);
    unmount();
    render(<Column {...pieces(variantById("vertical"))} />);
    // Comfortable: PTT precedes the status line.
    expect(order("ptt", "talker")).toBe(true);
  });

  it("takes the compact row path from density alone and still renders its chips", () => {
    const Row = SHELLS.row;
    const v = { ...variantById("horizontal"), density: "compact" as const };
    expect(v.shows.chips).toBe("enabled-only");
    const { unmount } = render(<Row {...pieces(v)} />);
    expect(screen.getByTestId("chips")).toBeInTheDocument();
    // Compact row: the LCD leads, ahead of the header (comfortable puts it below).
    expect(order("lcd", "rid")).toBe(true);
    unmount();
    render(<Row {...pieces(variantById("horizontal"))} />);
    expect(order("rid", "lcd")).toBe(true);
  });

  it("hides chips in a comfortable row that asks for none", () => {
    const Row = SHELLS.row;
    const v = {
      ...variantById("horizontal"),
      shows: { ...variantById("horizontal").shows, chips: "none" as const },
    };
    render(<Row {...pieces(v)} />);
    expect(screen.queryByTestId("chips")).toBeNull();
  });
});
