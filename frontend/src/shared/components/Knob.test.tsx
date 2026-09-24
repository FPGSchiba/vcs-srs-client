import { describe, expect, it, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { Knob } from "./Knob";

describe("Knob", () => {
  it("exposes its value to assistive tech", () => {
    render(<Knob value={70} onChange={() => {}} label="MASTER" />);
    const knob = screen.getByRole("slider", { name: /master/i });
    expect(knob).toHaveAttribute("aria-valuenow", "70");
    expect(knob).toHaveAttribute("aria-valuemin", "0");
    expect(knob).toHaveAttribute("aria-valuemax", "100");
  });

  it("exposes a custom min/max range", () => {
    render(
      <Knob value={0} onChange={() => {}} label="BAL" min={-50} max={50} />,
    );
    const knob = screen.getByRole("slider", { name: /bal/i });
    expect(knob).toHaveAttribute("aria-valuemin", "-50");
    expect(knob).toHaveAttribute("aria-valuemax", "50");
  });

  it("is reachable by keyboard (tabbable)", () => {
    render(<Knob value={70} onChange={() => {}} label="MASTER" />);
    const knob = screen.getByRole("slider", { name: /master/i });
    expect(knob).toHaveAttribute("tabindex", "0");
  });

  it("increases value by one on ArrowUp", () => {
    const onChange = vi.fn();
    render(<Knob value={70} onChange={onChange} label="MASTER" />);
    const knob = screen.getByRole("slider", { name: /master/i });
    fireEvent.keyDown(knob, { key: "ArrowUp" });
    expect(onChange).toHaveBeenCalledWith(71);
  });

  it("decreases value by one on ArrowDown", () => {
    const onChange = vi.fn();
    render(<Knob value={70} onChange={onChange} label="MASTER" />);
    const knob = screen.getByRole("slider", { name: /master/i });
    fireEvent.keyDown(knob, { key: "ArrowDown" });
    expect(onChange).toHaveBeenCalledWith(69);
  });

  it("jumps by ten on PageUp for coarse adjustment", () => {
    const onChange = vi.fn();
    render(<Knob value={50} onChange={onChange} label="MASTER" />);
    const knob = screen.getByRole("slider", { name: /master/i });
    fireEvent.keyDown(knob, { key: "PageUp" });
    expect(onChange).toHaveBeenCalledWith(60);
  });

  it("jumps by ten on PageDown for coarse adjustment", () => {
    const onChange = vi.fn();
    render(<Knob value={50} onChange={onChange} label="MASTER" />);
    const knob = screen.getByRole("slider", { name: /master/i });
    fireEvent.keyDown(knob, { key: "PageDown" });
    expect(onChange).toHaveBeenCalledWith(40);
  });

  it("clamps keyboard adjustment at the max", () => {
    const onChange = vi.fn();
    render(<Knob value={100} onChange={onChange} label="MASTER" />);
    const knob = screen.getByRole("slider", { name: /master/i });
    fireEvent.keyDown(knob, { key: "ArrowUp" });
    expect(onChange).toHaveBeenCalledWith(100);
  });

  it("clamps keyboard adjustment at the min", () => {
    const onChange = vi.fn();
    render(<Knob value={0} onChange={onChange} label="MASTER" />);
    const knob = screen.getByRole("slider", { name: /master/i });
    fireEvent.keyDown(knob, { key: "ArrowDown" });
    expect(onChange).toHaveBeenCalledWith(0);
  });

  it("jumps to min on Home and max on End", () => {
    const onChange = vi.fn();
    render(<Knob value={70} onChange={onChange} label="MASTER" />);
    const knob = screen.getByRole("slider", { name: /master/i });
    fireEvent.keyDown(knob, { key: "Home" });
    expect(onChange).toHaveBeenCalledWith(0);
    fireEvent.keyDown(knob, { key: "End" });
    expect(onChange).toHaveBeenCalledWith(100);
  });

  it("ignores unrelated keys", () => {
    const onChange = vi.fn();
    render(<Knob value={70} onChange={onChange} label="MASTER" />);
    const knob = screen.getByRole("slider", { name: /master/i });
    fireEvent.keyDown(knob, { key: "a" });
    expect(onChange).not.toHaveBeenCalled();
  });
});
