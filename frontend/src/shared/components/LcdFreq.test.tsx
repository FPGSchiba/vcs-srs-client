import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { LcdFreq } from "./LcdFreq";

// LcdFreq is read-only today only because Phase 1 had nothing to tune. The
// design prototype always specified drag/wheel/type editing. Editing types
// directly in kHz integers (the wire's ONLY canonical frequency
// representation, per internal/voice/freq.go) so repeated edits never
// accumulate floating-point drift in a displayed MHz float.
describe("LcdFreq", () => {
  it("commits a typed frequency on Enter", () => {
    const onChange = vi.fn();
    render(<LcdFreq value={118.5} onChange={onChange} />);
    const lcd = screen.getByRole("spinbutton");

    // Types "119000" (kHz) then commits.
    for (const key of "119000") fireEvent.keyDown(lcd, { key });
    fireEvent.keyDown(lcd, { key: "Enter" });

    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith(Math.fround(119000 / 1000));
  });

  it("reverts on Escape", () => {
    const onChange = vi.fn();
    render(<LcdFreq value={118.5} onChange={onChange} />);
    const lcd = screen.getByRole("spinbutton");

    for (const key of "999") fireEvent.keyDown(lcd, { key });
    fireEvent.keyDown(lcd, { key: "Escape" });

    expect(onChange).not.toHaveBeenCalled();
    // Display reverted to the original committed value.
    expect(lcd.textContent).toBe("118.500");

    // A later Enter with no new digits typed since the revert must not
    // fire a stale commit either.
    fireEvent.keyDown(lcd, { key: "Enter" });
    expect(onChange).not.toHaveBeenCalled();
  });

  it("steps by wheel", () => {
    const onChange = vi.fn();
    render(<LcdFreq value={118.5} onChange={onChange} />);
    const lcd = screen.getByRole("spinbutton");

    // Wheel up (negative deltaY) steps +1 kHz.
    fireEvent.wheel(lcd, { deltaY: -100 });
    expect(onChange).toHaveBeenNthCalledWith(1, Math.fround(118501 / 1000));

    // Wheel down (positive deltaY) steps -1 kHz from the ORIGINAL value
    // prop (the component is uncontrolled between renders in this test, so
    // `value` is still 118.5 until the parent re-renders with the new one).
    fireEvent.wheel(lcd, { deltaY: 100 });
    expect(onChange).toHaveBeenNthCalledWith(2, Math.fround(118499 / 1000));
  });

  it("clamps to the 24-bit kHz wire range", () => {
    const onChange = vi.fn();
    render(<LcdFreq value={118.5} onChange={onChange} />);
    const lcd = screen.getByRole("spinbutton");

    // 99,999,999 kHz is far past the 24-bit ceiling (16,777,215 kHz).
    for (const key of "99999999") fireEvent.keyDown(lcd, { key });
    fireEvent.keyDown(lcd, { key: "Enter" });

    expect(onChange).toHaveBeenCalledWith(Math.fround(16_777_215 / 1000));
  });

  it("stays read-only when no onChange is given", () => {
    const { container } = render(<LcdFreq value={118.5} />);
    const lcd = container.querySelector(".lcd-screen")!;

    expect(lcd).not.toHaveAttribute("role");
    expect(lcd).not.toHaveAttribute("tabindex");
    expect(lcd.textContent).toBe("118.500");

    // Typing/wheeling over a read-only LCD must be a no-op, not a crash.
    fireEvent.keyDown(lcd, { key: "9" });
    fireEvent.keyDown(lcd, { key: "Enter" });
    fireEvent.wheel(lcd, { deltaY: -100 });
    expect(lcd.textContent).toBe("118.500");
  });
});
