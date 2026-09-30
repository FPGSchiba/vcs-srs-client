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
  it("draws the status divider when the descriptor asks", () => {
    const Column = SHELLS.column;
    const { unmount } = render(<Column {...pieces(variantById("vertical"))} />);
    // toHaveStyle cannot see this: jsdom drops a var() inside a border shorthand,
    // so it compares against nothing (and the negative form passes vacuously).
    // Read the inline style attribute instead.
    const divider = "border-top: 1px solid var(--bd-1)";
    expect(screen.getByTestId("shell-status").getAttribute("style")).toContain(divider);
    unmount();
    render(<Column {...pieces(variantById("narrow-v"))} />);
    expect(screen.getByTestId("shell-status").getAttribute("style") ?? "").not.toContain(
      "border-top",
    );
  });

  it("left-aligns the narrow-v name beside the rid rather than centring it", () => {
    const Column = SHELLS.column;
    render(<Column {...pieces(variantById("narrow-v"))} />);
    const header = screen.getByTestId("shell-header");
    expect(header).toHaveStyle({ justifyContent: "flex-start" });
    // rid first, then the name slot (which wraps the name).
    const kids = within(header).getAllByTestId(/^(rid|shell-name-slot)$/);
    expect(kids.map((k) => k.getAttribute("data-testid"))).toEqual(["rid", "shell-name-slot"]);
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
