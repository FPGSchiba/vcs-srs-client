import { fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { RadioCard } from "./RadioCard";
import { VARIANTS } from "./variants";
import { nextRadioName } from "./radioName";
import { api, type RadioDTO } from "../../shared/api/client";
import { useRadios } from "../../shared/store/radios";

// A fifth registry entry: the drawer must list it with no code change.
vi.mock("./variants", async (orig) => {
  const actual = await orig<typeof import("./variants")>();
  return {
    ...actual,
    VARIANTS: [...actual.VARIANTS, { ...actual.VARIANTS[0], id: "fifth", label: "Fifth" }],
  };
});

const radio = {
  id: 1,
  name: "Fleet Common",
  frequency: 118.5,
  enabled: true,
  is_intercom: false,
} as RadioDTO;

const origW = window.innerWidth;
const origH = window.innerHeight;

beforeEach(() => {
  vi.restoreAllMocks();
  useRadios.setState({ selectedRadioId: 0, heldPTT: new Set(), globalPttTargetId: 0 });
});
afterEach(() => {
  Object.defineProperty(window, "innerWidth", { value: origW, configurable: true });
  Object.defineProperty(window, "innerHeight", { value: origH, configurable: true });
});

const show = (variantId = "vertical", r: Partial<RadioDTO> = {}, onVariantChange = vi.fn()) => {
  const full = { ...radio, ...r };
  render(
    <RadioCard
      radio={full}
      allRadios={[full]}
      muted={false}
      variantId={variantId}
      onVariantChange={onVariantChange}
    />,
  );
  return { full, onVariantChange };
};
const gear = () => screen.getByRole("button", { name: "Radio settings" });
const open = () => fireEvent.click(gear());
const stubUpdate = () => vi.spyOn(api, "updateRadioInfo").mockResolvedValue(undefined as never);

function gearRect(left: number, top: number, vw: number, vh: number) {
  Object.defineProperty(window, "innerWidth", { value: vw, configurable: true });
  Object.defineProperty(window, "innerHeight", { value: vh, configurable: true });
  const rect = { left, right: left + 16, top, bottom: top + 16, width: 16, height: 16, x: left, y: top };
  vi.spyOn(gear(), "getBoundingClientRect").mockReturnValue({ ...rect, toJSON() {} } as DOMRect);
}

describe("the settings gear", () => {
  it.each(VARIANTS.map((v) => v.id))("renders on %s", (id) => {
    show(id);
    expect(gear()).toHaveAttribute("type", "button");
    expect(gear()).toHaveAttribute("aria-haspopup", "dialog");
    expect(gear()).toHaveAttribute("aria-expanded", "false");
  });

  it("has a visible resting border and a brighter glyph", () => {
    show("vertical");
    expect(gear().style.border).toContain("var(--bd-2)");
    expect(gear().style.background).toContain("var(--bg-2)");
    expect(gear().style.color).toBe("var(--tx-1)");
    expect(gear().style.width).toBe("16px");
    expect(gear().style.height).toBe("16px");
    expect(gear().style.boxSizing).toBe("border-box");
  });

  it("no longer puts the toggles on the card", () => {
    show("vertical");
    expect(screen.queryByRole("switch")).toBeNull();
  });
});

describe("the drawer", () => {
  it("opens on click as a named dialog", () => {
    show();
    open();
    expect(screen.getByRole("dialog", { name: /radio settings/i })).toBeInTheDocument();
    expect(gear()).toHaveAttribute("aria-expanded", "true");
  });

  // jsdom ignores `visibility`, but a real browser will not focus a hidden
  // element: so assert the panel is already visible at the moment focus() runs.
  it("focuses the panel only once it is visible and positioned", () => {
    const focused: { role: string | null; visibility: string }[] = [];
    const real = HTMLElement.prototype.focus;
    vi.spyOn(HTMLElement.prototype, "focus").mockImplementation(function (this: HTMLElement, o) {
      focused.push({ role: this.getAttribute("role"), visibility: this.style.visibility });
      real.call(this, o);
    });
    show();
    open();
    const dlg = focused.filter((f) => f.role === "dialog");
    expect(dlg.length).toBeGreaterThan(0);
    for (const f of dlg) expect(f.visibility).toBe("visible");
    expect(document.activeElement).toBe(screen.getByRole("dialog"));
  });

  it("is not a descendant of the card's clipping frame", () => {
    show();
    open();
    expect(screen.getByRole("option", { name: "R01 Fleet Common" }).contains(screen.getByRole("dialog"))).toBe(false);
    expect(screen.getByRole("dialog").parentElement).toBe(document.body);
  });

  it("closes on Escape from inside the panel and returns focus to the gear", () => {
    show();
    open();
    fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.activeElement).toBe(gear());
  });

  it("closes on Escape even when focus is not in the panel", () => {
    show();
    open();
    gear().focus(); // a browser that did not move focus in
    fireEvent.keyDown(document.body, { key: "Escape" });
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.activeElement).toBe(gear());
  });

  it("closes on an outside pointerdown, returning focus, but not an inside one", () => {
    show();
    open();
    fireEvent.pointerDown(screen.getByRole("dialog"));
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    fireEvent.pointerDown(document.body);
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.activeElement).toBe(gear());
  });

  it("does not close-then-reopen when the pointer goes down on the gear", () => {
    show();
    open();
    fireEvent.pointerDown(gear());
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    fireEvent.click(gear());
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("closes on an element scroll (capture) and returns focus", () => {
    show();
    open();
    fireEvent.scroll(screen.getByRole("option", { name: "R01 Fleet Common" })); // scroll does not bubble
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.activeElement).toBe(gear());
  });

  it("does not close when the scroll is inside the drawer", () => {
    show();
    open();
    fireEvent.scroll(screen.getByRole("textbox"));
    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });

  it("closes on window resize and returns focus", () => {
    show();
    open();
    fireEvent(window, new Event("resize"));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.activeElement).toBe(gear());
  });

  it("toggles closed when the gear is clicked again", () => {
    show();
    open();
    open();
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("removes exactly the listeners it added, capture flag included", () => {
    const add = vi.spyOn(window, "addEventListener");
    const remove = vi.spyOn(window, "removeEventListener");
    const dadd = vi.spyOn(document, "addEventListener");
    const dremove = vi.spyOn(document, "removeEventListener");
    show();
    open();
    fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
    const mine = (c: unknown[][]) => c.filter(([t]) => ["scroll", "resize", "pointerdown", "keydown"].includes(t as string));
    const pairs = [
      [mine(add.mock.calls), mine(remove.mock.calls)],
      [mine(dadd.mock.calls), mine(dremove.mock.calls)],
    ];
    let total = 0;
    for (const [added, removed] of pairs) {
      for (const a of added) {
        total++;
        expect(removed.some((r) => r[0] === a[0] && r[1] === a[1] && r[2] === a[2]), `${String(a[0])} ${String(a[2])}`).toBe(true);
      }
    }
    expect(total).toBeGreaterThanOrEqual(4);
    const scroll = add.mock.calls.find(([t]) => t === "scroll");
    expect(scroll?.[2]).toBe(true);
  });
});

describe("positioning", () => {
  it("right-aligns under the gear", () => {
    show();
    gearRect(500, 100, 1024, 800);
    open();
    const s = screen.getByRole("dialog").style;
    expect(Number.parseFloat(s.left)).toBe(500 + 16 - 220);
    expect(Number.parseFloat(s.top)).toBe(100 + 16 + 4);
  });

  it("clamps to the right edge when the gear is at the viewport edge", () => {
    show();
    gearRect(1014, 100, 1024, 800); // right = 1030 > vw: unclamped left would be 810
    open();
    const left = Number.parseFloat(screen.getByRole("dialog").style.left);
    expect(left).toBe(1024 - 220 - 8);
  });

  it("clamps to the left edge for a card near the left of the window", () => {
    show("narrow-v");
    gearRect(20, 100, 1024, 800); // right - 220 is negative
    open();
    expect(Number.parseFloat(screen.getByRole("dialog").style.left)).toBe(8);
  });

  it("flips above the gear near the bottom", () => {
    show();
    gearRect(500, 760, 1024, 800);
    open();
    expect(Number.parseFloat(screen.getByRole("dialog").style.top)).toBeLessThan(760);
  });
});

describe("what the drawer commits", () => {
  it("renames through api.updateRadioInfo", () => {
    const spy = stubUpdate();
    const { full } = show();
    open();
    const input = screen.getByRole("textbox");
    fireEvent.change(input, { target: { value: "Ops" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(spy).toHaveBeenCalledWith({ muted: false, radios: [{ ...full, name: "Ops" }] });
  });

  it("commits a name once when Enter is followed by a blur", () => {
    const spy = stubUpdate();
    show();
    open();
    const input = screen.getByRole("textbox");
    fireEvent.change(input, { target: { value: "Ops" } });
    fireEvent.keyDown(input, { key: "Enter" });
    fireEvent.blur(input);
    expect(spy).toHaveBeenCalledTimes(1);
  });

  it("sets enabled through api.updateRadioInfo", () => {
    const spy = stubUpdate();
    const { full } = show();
    open();
    fireEvent.click(screen.getByRole("switch", { name: /enabled/i }));
    expect(spy).toHaveBeenCalledWith({ muted: false, radios: [{ ...full, enabled: false }] });
  });

  it("sets intercom through api.updateRadioInfo", () => {
    const spy = stubUpdate();
    const { full } = show();
    open();
    fireEvent.click(screen.getByRole("switch", { name: /intercom/i }));
    expect(spy).toHaveBeenCalledWith({ muted: false, radios: [{ ...full, is_intercom: true }] });
  });

  it("operates a toggle from its visible label text", () => {
    const spy = stubUpdate();
    show();
    open();
    fireEvent.click(screen.getByText("Intercom"));
    expect(spy).toHaveBeenCalledTimes(1);
  });

  it("never updates the radios store optimistically", () => {
    stubUpdate();
    const before = useRadios.getState().radios;
    show();
    open();
    fireEvent.click(screen.getByRole("switch", { name: /enabled/i }));
    fireEvent.click(screen.getByRole("switch", { name: /intercom/i }));
    const input = screen.getByRole("textbox");
    fireEvent.change(input, { target: { value: "Ops" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(useRadios.getState().radios).toBe(before);
  });

  it("lists every registry variant, including one added to the registry", () => {
    const spy = stubUpdate();
    const { onVariantChange } = show("vertical");
    open();
    const select = screen.getByRole("combobox", { name: /variant/i });
    const options = within(select).getAllByRole("option").map((o) => (o as HTMLOptionElement).value);
    expect(options).toEqual(VARIANTS.map((v) => v.id));
    expect(options).toContain("fifth");
    expect(within(select).getByRole("option", { name: "Fifth" })).toBeInTheDocument();
    expect(select).toHaveValue("vertical");
    fireEvent.change(select, { target: { value: "horizontal" } });
    expect(onVariantChange).toHaveBeenCalledWith("horizontal");
    expect(spy).not.toHaveBeenCalled();
  });

  it("closes and returns focus to the gear when a variant is chosen", () => {
    show("vertical");
    open();
    fireEvent.change(screen.getByRole("combobox", { name: /variant/i }), { target: { value: VARIANTS[2].id } });
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.activeElement).toBe(gear());
  });
});

describe("the name draft", () => {
  it("is reset from the radio when the drawer reopens", () => {
    show();
    open();
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "abandoned" } });
    fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
    open();
    expect(screen.getByRole("textbox")).toHaveValue("Fleet Common");
  });

  // jsdom only blurs on dismissal if the input really has focus, so focus it.
  const typeFocused = () => {
    const input = screen.getByRole("textbox");
    input.focus();
    fireEvent.change(input, { target: { value: "half-typed" } });
  };

  it("discards the draft on Escape instead of committing it", () => {
    const spy = stubUpdate();
    show();
    open();
    typeFocused();
    fireEvent.keyDown(document.body, { key: "Escape" });
    expect(spy).not.toHaveBeenCalled();
    open();
    expect(screen.getByRole("textbox")).toHaveValue("Fleet Common");
  });

  it("discards the draft on an outside pointerdown instead of committing it", () => {
    const spy = stubUpdate();
    show();
    open();
    typeFocused();
    fireEvent.pointerDown(document.body);
    expect(spy).not.toHaveBeenCalled();
    open();
    expect(screen.getByRole("textbox")).toHaveValue("Fleet Common");
  });

  it("still commits when focus moves elsewhere inside the drawer", () => {
    const spy = stubUpdate();
    show();
    open();
    typeFocused();
    screen.getByRole("switch", { name: /enabled/i }).focus();
    expect(spy).toHaveBeenCalledTimes(1);
    expect(spy.mock.calls[0][0].radios[0].name).toBe("half-typed");
  });

  it("rejects an all-whitespace name in the drawer", () => {
    const spy = stubUpdate();
    show();
    open();
    const input = screen.getByRole("textbox");
    fireEvent.change(input, { target: { value: "   " } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(spy).not.toHaveBeenCalled();
    expect(input).toHaveValue("Fleet Common");
  });

  it("rejects an all-whitespace name in the inline editor too", () => {
    const spy = stubUpdate();
    show();
    fireEvent.doubleClick(screen.getByText("Fleet Common", { selector: "span" }));
    const input = screen.getByRole("textbox", { name: "radio name" });
    fireEvent.change(input, { target: { value: "   " } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(spy).not.toHaveBeenCalled();
  });

  it("shares one rule between both paths", () => {
    expect(nextRadioName("  Ops ", "Fleet")).toBe("Ops");
    expect(nextRadioName("   ", "Fleet")).toBeNull();
    expect(nextRadioName("Fleet", "Fleet")).toBeNull();
  });
});

describe("events do not leak through the portal", () => {
  it("stops click and drag events at the panel", () => {
    const drag = vi.fn();
    const click = vi.fn();
    render(
      <div onDragStart={drag} onDragOver={drag} onDrop={drag} onClick={click}>
        <RadioCard radio={radio} allRadios={[radio]} muted={false} variantId="vertical" onVariantChange={() => {}} />
      </div>,
    );
    open();
    click.mockClear();
    const dlg = screen.getByRole("dialog");
    fireEvent.click(dlg);
    fireEvent.dragStart(dlg);
    fireEvent.dragOver(dlg);
    fireEvent.drop(dlg);
    expect(click).not.toHaveBeenCalled();
    expect(drag).not.toHaveBeenCalled();
  });
});

describe("state stays readable from the frame", () => {
  it("exposes disabled and intercom as data attributes", () => {
    show("narrow-h", { enabled: false, is_intercom: true });
    const frame = screen.getByRole("option");
    expect(frame).toHaveAttribute("data-disabled", "true");
    expect(frame).toHaveAttribute("data-intercom", "true");
  });
});
