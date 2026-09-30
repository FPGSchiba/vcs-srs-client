import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { RadioCard } from "./RadioCard";
import { VARIANTS } from "./variants";
import { api, type RadioDTO } from "../../shared/api/client";
import { useRadios } from "../../shared/store/radios";

const radio = {
  id: 1,
  name: "Fleet Common",
  frequency: 118.5,
  enabled: true,
  is_intercom: false,
} as RadioDTO;

beforeEach(() => {
  vi.restoreAllMocks();
  useRadios.setState({ selectedRadioId: 0, heldPTT: new Set(), globalPttTargetId: 0 });
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

describe("the settings gear", () => {
  it.each(VARIANTS.map((v) => v.id))("renders on %s", (id) => {
    show(id);
    expect(gear()).toHaveAttribute("type", "button");
    expect(gear()).toHaveAttribute("aria-haspopup", "dialog");
    expect(gear()).toHaveAttribute("aria-expanded", "false");
  });

  it("no longer puts the toggles on the card", () => {
    show("vertical");
    expect(screen.queryByRole("switch")).toBeNull();
  });
});

describe("the drawer", () => {
  it("opens on click as a named dialog and moves focus into it", () => {
    show();
    open();
    const dlg = screen.getByRole("dialog", { name: /radio settings/i });
    expect(gear()).toHaveAttribute("aria-expanded", "true");
    expect(dlg.contains(document.activeElement)).toBe(true);
  });

  it("is not a descendant of the card's clipping frame", () => {
    show();
    open();
    expect(screen.getByRole("option").contains(screen.getByRole("dialog"))).toBe(false);
    expect(screen.getByRole("dialog").parentElement).toBe(document.body);
  });

  it("closes on Escape and returns focus to the gear", () => {
    show();
    open();
    fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.activeElement).toBe(gear());
  });

  it("returns focus to the gear on any close", () => {
    show();
    open();
    fireEvent.pointerDown(document.body);
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.activeElement).toBe(gear());
  });

  it("closes on an outside pointerdown but not an inside one", () => {
    show();
    open();
    fireEvent.pointerDown(screen.getByRole("dialog"));
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    fireEvent.pointerDown(document.body);
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("closes on scroll and on window resize", () => {
    show();
    open();
    fireEvent.scroll(window);
    expect(screen.queryByRole("dialog")).toBeNull();
    open();
    fireEvent(window, new Event("resize"));
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("toggles closed when the gear is clicked again", () => {
    show();
    open();
    open();
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("removes its listeners when it closes", () => {
    const add = vi.spyOn(window, "addEventListener");
    const remove = vi.spyOn(window, "removeEventListener");
    show();
    open();
    fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
    const added = add.mock.calls.filter(([t]) => ["scroll", "resize"].includes(t as string));
    const removed = remove.mock.calls.filter(([t]) => ["scroll", "resize"].includes(t as string));
    expect(added.length).toBeGreaterThan(0);
    expect(removed.length).toBe(added.length);
  });

  it("stays inside the viewport when the gear is near the right/bottom edge", () => {
    show();
    const rect = { left: 1000, right: 1016, top: 760, bottom: 776, width: 16, height: 16, x: 1000, y: 760 };
    vi.spyOn(gear(), "getBoundingClientRect").mockReturnValue({ ...rect, toJSON() {} } as DOMRect);
    Object.defineProperty(window, "innerWidth", { value: 1024, configurable: true });
    Object.defineProperty(window, "innerHeight", { value: 800, configurable: true });
    open();
    const s = screen.getByRole("dialog").style;
    expect(Number.parseFloat(s.left) + 200).toBeLessThanOrEqual(1024);
    expect(Number.parseFloat(s.top)).toBeLessThan(760); // flipped above the gear
  });
});

describe("what the drawer commits", () => {
  it("renames through api.updateRadioInfo", () => {
    const spy = vi.spyOn(api, "updateRadioInfo").mockResolvedValue(undefined as never);
    const { full } = show();
    open();
    const input = screen.getByRole("textbox", { name: /name/i });
    fireEvent.change(input, { target: { value: "Ops" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(spy).toHaveBeenCalledWith({ muted: false, radios: [{ ...full, name: "Ops" }] });
  });

  it("sets enabled through api.updateRadioInfo", () => {
    const spy = vi.spyOn(api, "updateRadioInfo").mockResolvedValue(undefined as never);
    const { full } = show();
    open();
    fireEvent.click(screen.getByRole("switch", { name: /enabled/i }));
    expect(spy).toHaveBeenCalledWith({ muted: false, radios: [{ ...full, enabled: false }] });
  });

  it("sets intercom through api.updateRadioInfo", () => {
    const spy = vi.spyOn(api, "updateRadioInfo").mockResolvedValue(undefined as never);
    const { full } = show();
    open();
    fireEvent.click(screen.getByRole("switch", { name: /intercom/i }));
    expect(spy).toHaveBeenCalledWith({ muted: false, radios: [{ ...full, is_intercom: true }] });
  });

  it("does not update the radios store optimistically", () => {
    vi.spyOn(api, "updateRadioInfo").mockResolvedValue(undefined as never);
    show();
    open();
    fireEvent.click(screen.getByRole("switch", { name: /enabled/i }));
    expect(radio.enabled).toBe(true);
  });

  it("offers every registry variant and reports the choice via onVariantChange", () => {
    const spy = vi.spyOn(api, "updateRadioInfo").mockResolvedValue(undefined as never);
    const { onVariantChange } = show("vertical");
    open();
    for (const v of VARIANTS) expect(screen.getByRole("radio", { name: v.label })).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: "Vertical" })).toBeChecked();
    fireEvent.click(screen.getByRole("radio", { name: "Horizontal" }));
    expect(onVariantChange).toHaveBeenCalledWith("horizontal");
    expect(spy).not.toHaveBeenCalled();
  });
});

describe("state stays readable from the frame without the chips", () => {
  it("exposes disabled and intercom as data attributes", () => {
    show("narrow-h", { enabled: false, is_intercom: true });
    const frame = screen.getByRole("option");
    expect(frame).toHaveAttribute("data-disabled", "true");
    expect(frame).toHaveAttribute("data-intercom", "true");
  });
});
