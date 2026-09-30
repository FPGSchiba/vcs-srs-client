import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { LcdFreq } from "./LcdFreq";
import { MAX_KHZ } from "../freq";

const digits = () => screen.getAllByRole("button", { name: /digit/i });

describe("display", () => {
  it("renders the kHz value as padded digits", () => {
    render(<LcdFreq khz={118_500} />);
    expect(screen.getByRole("group", { name: "frequency" }).textContent).toBe("118.500");
  });

  it("renders the MHZ unit only when asked", () => {
    const { rerender } = render(<LcdFreq khz={118_500} unit />);
    expect(screen.getByText("MHZ")).toBeInTheDocument();
    rerender(<LcdFreq khz={118_500} />);
    expect(screen.queryByText("MHZ")).toBeNull();
  });

  it("sizes the digits from digitPx", () => {
    render(<LcdFreq khz={118_500} digitPx={30} />);
    expect(screen.getByRole("group", { name: "frequency" })).toHaveStyle({ fontSize: "30px" });
  });

  it("is read-only with no onChange: no digit buttons, no spinbutton", () => {
    render(<LcdFreq khz={118_500} />);
    expect(screen.queryAllByRole("button")).toHaveLength(0);
    expect(screen.queryByRole("spinbutton")).toBeNull();
  });
});

describe("per-digit editing", () => {
  it("wheel over a digit steps that decade only", () => {
    const onChange = vi.fn();
    render(<LcdFreq khz={118_500} onChange={onChange} />);
    // Cells are "1","1","8",".","5","0","0"; digits()[3] is the "5" (100 kHz).
    fireEvent.wheel(digits()[3], { deltaY: -1 });
    expect(onChange).toHaveBeenCalledWith(118_600);
  });

  it("wheel down steps that decade down", () => {
    const onChange = vi.fn();
    render(<LcdFreq khz={118_500} onChange={onChange} />);
    fireEvent.wheel(digits()[2], { deltaY: 1 }); // the "8": 1 MHz decade
    expect(onChange).toHaveBeenCalledWith(117_500);
  });

  it("click bumps up and shift-click bumps down", () => {
    const onChange = vi.fn();
    render(<LcdFreq khz={118_500} onChange={onChange} />);
    fireEvent.click(digits()[5]); // last digit: 1 kHz
    expect(onChange).toHaveBeenLastCalledWith(118_501);
    fireEvent.click(digits()[5], { shiftKey: true });
    expect(onChange).toHaveBeenLastCalledWith(118_499);
  });

  it("does not select the card when a digit is clicked", () => {
    const onCardClick = vi.fn();
    render(
      <div onClick={onCardClick}>
        <LcdFreq khz={118_500} onChange={vi.fn()} />
      </div>,
    );
    fireEvent.click(digits()[5]);
    expect(onCardClick).not.toHaveBeenCalled();
  });

  // Review Focus #2, at the UI boundary.
  it("a refused step at the ceiling emits nothing rather than a rewritten value", () => {
    const onChange = vi.fn();
    render(<LcdFreq khz={MAX_KHZ} onChange={onChange} />);
    fireEvent.wheel(digits()[digits().length - 1], { deltaY: -1 });
    expect(onChange).not.toHaveBeenCalled();
  });
});

describe("keyboard", () => {
  it("is one tab stop carrying spinbutton semantics", () => {
    render(<LcdFreq khz={118_500} onChange={vi.fn()} />);
    const lcd = screen.getByRole("spinbutton", { name: "frequency" });
    expect(lcd).toHaveAttribute("tabindex", "0");
    for (const d of digits()) expect(d).toHaveAttribute("tabindex", "-1");
  });

  it("ArrowUp steps the digit under the cursor, which starts at 1 kHz", () => {
    const onChange = vi.fn();
    render(<LcdFreq khz={118_500} onChange={onChange} />);
    fireEvent.keyDown(screen.getByRole("spinbutton"), { key: "ArrowUp" });
    expect(onChange).toHaveBeenCalledWith(118_501);
  });

  it("ArrowLeft moves the cursor one decade up before stepping", () => {
    const onChange = vi.fn();
    render(<LcdFreq khz={118_500} onChange={onChange} />);
    const lcd = screen.getByRole("spinbutton");
    fireEvent.keyDown(lcd, { key: "ArrowLeft" });
    fireEvent.keyDown(lcd, { key: "ArrowUp" });
    expect(onChange).toHaveBeenCalledWith(118_510);
  });

  it("ArrowLeft skips the separator", () => {
    const onChange = vi.fn();
    render(<LcdFreq khz={118_500} onChange={onChange} />);
    const lcd = screen.getByRole("spinbutton");
    for (let i = 0; i < 3; i++) fireEvent.keyDown(lcd, { key: "ArrowLeft" });
    fireEvent.keyDown(lcd, { key: "ArrowUp" });
    expect(onChange).toHaveBeenCalledWith(119_500); // 1 MHz, not the ".".
  });

  it("marks the cursor digit so it is visible", () => {
    render(<LcdFreq khz={118_500} onChange={vi.fn()} />);
    fireEvent.keyDown(screen.getByRole("spinbutton"), { key: "ArrowLeft" });
    const cursored = digits().filter((d) => d.getAttribute("data-cursor") === "true");
    expect(cursored).toHaveLength(1);
    expect(cursored[0].textContent).toBe("0"); // the 10 kHz digit
  });
});

describe("typed entry still works", () => {
  it("builds a draft and commits on Enter", () => {
    const onChange = vi.fn();
    render(<LcdFreq khz={118_500} onChange={onChange} />);
    const lcd = screen.getByRole("spinbutton");
    for (const k of ["1", "2", "3", "0", "0", "0"]) fireEvent.keyDown(lcd, { key: k });
    fireEvent.keyDown(lcd, { key: "Enter" });
    expect(onChange).toHaveBeenCalledWith(123_000);
  });

  it("Escape discards the draft", () => {
    const onChange = vi.fn();
    render(<LcdFreq khz={118_500} onChange={onChange} />);
    const lcd = screen.getByRole("spinbutton");
    fireEvent.keyDown(lcd, { key: "9" });
    fireEvent.keyDown(lcd, { key: "Escape" });
    expect(onChange).not.toHaveBeenCalled();
    expect(screen.getByRole("group", { name: "frequency" }).textContent).toBe("118.500");
  });

  it("blur discards the draft", () => {
    const onChange = vi.fn();
    render(<LcdFreq khz={118_500} onChange={onChange} />);
    const lcd = screen.getByRole("spinbutton");
    fireEvent.keyDown(lcd, { key: "9" });
    fireEvent.blur(lcd);
    expect(onChange).not.toHaveBeenCalled();
  });

  it("per-digit stepping is inert while a draft is open", () => {
    const onChange = vi.fn();
    render(<LcdFreq khz={118_500} onChange={onChange} />);
    const lcd = screen.getByRole("spinbutton");
    fireEvent.keyDown(lcd, { key: "9" });
    // A draft renders no digit buttons at all (its cells have no place), so
    // the click path is unreachable; the keyboard step path is the live one.
    expect(screen.queryAllByRole("button")).toHaveLength(0);
    fireEvent.keyDown(lcd, { key: "ArrowUp" });
    expect(onChange).not.toHaveBeenCalled();
  });
});
