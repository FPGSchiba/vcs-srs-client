import { describe, expect, it, vi } from "vitest";
import { render, screen, fireEvent, act } from "@testing-library/react";
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

  // FE15: the drag is effect-scoped, so it ends on pointercancel and on
  // unmount, not only on pointerup.
  it("pointercancel ends the drag", () => {
    const onChange = vi.fn();
    render(<Knob value={50} onChange={onChange} label="MASTER" />);
    const knob = screen.getByRole("slider", { name: /master/i });
    fireEvent.pointerDown(knob, { clientY: 100 });
    fireEvent.pointerMove(window, { clientY: 90 });
    expect(onChange).toHaveBeenCalledTimes(1);
    fireEvent.pointerCancel(window);
    onChange.mockClear();
    fireEvent.pointerMove(window, { clientY: 80 });
    expect(onChange).not.toHaveBeenCalled();
  });

  it("unmount mid-drag removes window listeners", () => {
    const onChange = vi.fn();
    const { unmount } = render(<Knob value={50} onChange={onChange} label="MASTER" />);
    const knob = screen.getByRole("slider", { name: /master/i });
    fireEvent.pointerDown(knob, { clientY: 100 });
    unmount();
    fireEvent.pointerMove(window, { clientY: 80 });
    expect(onChange).not.toHaveBeenCalled();
  });

  it("renders the draft while dragging", () => {
    render(<Knob value={50} onChange={() => {}} label="MASTER" />);
    const knob = screen.getByRole("slider", { name: /master/i });
    fireEvent.pointerDown(knob, { clientY: 100 });
    act(() => {
      fireEvent.pointerMove(window, { clientY: 80 });
    });
    // 20px of upward travel over a 100px full range is +20.
    expect(knob).toHaveAttribute("aria-valuenow", "70");
    act(() => {
      fireEvent.pointerUp(window);
    });
    // Released: the controlled value shows again.
    expect(knob).toHaveAttribute("aria-valuenow", "50");
  });

  it("onChangeEnd fires once per drag", () => {
    const onChangeEnd = vi.fn();
    render(<Knob value={50} onChange={() => {}} onChangeEnd={onChangeEnd} label="MASTER" />);
    const knob = screen.getByRole("slider", { name: /master/i });
    fireEvent.pointerDown(knob, { clientY: 100 });
    fireEvent.pointerMove(window, { clientY: 95 });
    fireEvent.pointerMove(window, { clientY: 90 });
    fireEvent.pointerUp(window);
    fireEvent.pointerUp(window);
    expect(onChangeEnd).toHaveBeenCalledTimes(1);
    expect(onChangeEnd).toHaveBeenCalledWith(60);
  });

  it("onChangeEnd fires on keyup, not on each keydown", () => {
    const onChange = vi.fn();
    const onChangeEnd = vi.fn();
    render(<Knob value={50} onChange={onChange} onChangeEnd={onChangeEnd} label="MASTER" />);
    const knob = screen.getByRole("slider", { name: /master/i });
    fireEvent.keyDown(knob, { key: "ArrowUp" });
    fireEvent.keyDown(knob, { key: "ArrowUp" });
    expect(onChange).toHaveBeenCalledTimes(2);
    expect(onChangeEnd).not.toHaveBeenCalled();
    // With onChangeEnd the Knob steps from its own draft, so auto-repeat
    // keeps climbing even while a throttled parent has not re-rendered yet.
    expect(onChange).toHaveBeenLastCalledWith(52);
    fireEvent.keyUp(knob, { key: "ArrowUp" });
    expect(onChangeEnd).toHaveBeenCalledTimes(1);
    expect(onChangeEnd).toHaveBeenCalledWith(52);
    fireEvent.keyUp(knob, { key: "a" });
    expect(onChangeEnd).toHaveBeenCalledTimes(1);
  });
});
