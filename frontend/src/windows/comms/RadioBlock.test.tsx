import { render, screen, fireEvent } from "@testing-library/react";
import { StrictMode } from "react";
import { describe, expect, it, vi } from "vitest";
import { RadioBlock } from "./RadioBlock";
import type { RadioDTO } from "../../shared/api/client";

const radio: RadioDTO = {
  id: 1,
  name: "Fleet",
  frequency: 118.5,
  enabled: true,
  is_intercom: false,
};

function block(over: Partial<React.ComponentProps<typeof RadioBlock>> = {}) {
  return (
    <RadioBlock radio={radio} allRadios={[radio]} muted={false}
      width={400} height={150} index={0} onResize={vi.fn()} onReorder={vi.fn()} {...over} />
  );
}

function fakeDataTransfer(store: Record<string, string>) {
  return {
    setData: (k: string, v: string) => { store[k] = v; },
    getData: (k: string) => store[k] ?? "",
    types: ["text/plain"],
    effectAllowed: "",
    dropEffect: "",
  };
}

describe("RadioBlock", () => {
  it("renders at the size it is given", () => {
    const { container } = render(block());
    const el = container.firstElementChild as HTMLElement;
    expect(el.style.width).toBe("400px");
    expect(el.style.height).toBe("150px");
  });

  it("reports a resize on pointer drag", () => {
    const onResize = vi.fn();
    render(block({ onResize }));
    const handle = screen.getByRole("button", { name: /resize/i });
    fireEvent.pointerDown(handle, { clientX: 400, clientY: 150, pointerId: 1 });
    fireEvent.pointerMove(window, { clientX: 460, clientY: 190, pointerId: 1 });
    fireEvent.pointerUp(window, { pointerId: 1 });
    expect(onResize).toHaveBeenCalledWith(1, 460, 190);
  });

  it("clamps a resize to the minimum block size", () => {
    const onResize = vi.fn();
    render(block({ onResize }));
    const handle = screen.getByRole("button", { name: /resize/i });
    fireEvent.pointerDown(handle, { clientX: 400, clientY: 150, pointerId: 1 });
    fireEvent.pointerMove(window, { clientX: 10, clientY: 10, pointerId: 1 });
    fireEvent.pointerUp(window, { pointerId: 1 });
    const [, w, h] = onResize.mock.calls.at(-1)!;
    expect(w).toBeGreaterThanOrEqual(240);
    expect(h).toBeGreaterThanOrEqual(96);
  });

  it("clamps width and height independently, to exactly the minimum", () => {
    const onResize = vi.fn();
    render(block({ onResize }));
    const handle = screen.getByRole("button", { name: /resize/i });
    fireEvent.pointerDown(handle, { clientX: 400, clientY: 150, pointerId: 1 });
    // Only width collapses: height must follow the pointer untouched.
    fireEvent.pointerMove(window, { clientX: 0, clientY: 200, pointerId: 1 });
    expect(onResize).toHaveBeenLastCalledWith(1, 240, 200);
    // Only height collapses.
    fireEvent.pointerMove(window, { clientX: 500, clientY: 0, pointerId: 1 });
    expect(onResize).toHaveBeenLastCalledWith(1, 500, 96);
    fireEvent.pointerUp(window, { pointerId: 1 });
  });

  it("commits the final size again on pointer up", () => {
    const onResize = vi.fn();
    render(block({ onResize }));
    const handle = screen.getByRole("button", { name: /resize/i });
    fireEvent.pointerDown(handle, { clientX: 400, clientY: 150, pointerId: 1 });
    fireEvent.pointerMove(window, { clientX: 460, clientY: 190, pointerId: 1 });
    onResize.mockClear();
    fireEvent.pointerUp(window, { pointerId: 1 });
    expect(onResize).toHaveBeenCalledTimes(1);
    expect(onResize).toHaveBeenCalledWith(1, 460, 190);
  });

  it("stops resizing after pointer up", () => {
    const onResize = vi.fn();
    render(block({ onResize }));
    const handle = screen.getByRole("button", { name: /resize/i });
    fireEvent.pointerDown(handle, { clientX: 400, clientY: 150, pointerId: 1 });
    fireEvent.pointerMove(window, { clientX: 450, clientY: 150, pointerId: 1 });
    fireEvent.pointerUp(window, { pointerId: 1 });
    onResize.mockClear();
    fireEvent.pointerMove(window, { clientX: 600, clientY: 300, pointerId: 1 });
    expect(onResize).not.toHaveBeenCalled();
  });

  it("does not resize on pointer movement that never started on the handle", () => {
    const onResize = vi.fn();
    render(block({ onResize }));
    fireEvent.pointerMove(window, { clientX: 600, clientY: 300, pointerId: 1 });
    expect(onResize).not.toHaveBeenCalled();
  });

  it("resizes with the keyboard", () => {
    // S1082: the handle is a non-button element with a pointer handler, so
    // it must also be operable without a pointer.
    const onResize = vi.fn();
    render(block({ onResize }));
    const handle = screen.getByRole("button", { name: /resize/i });
    expect(handle).toHaveAttribute("tabindex", "0");
    fireEvent.keyDown(handle, { key: "ArrowRight" });
    expect(onResize).toHaveBeenLastCalledWith(1, 416, 150);
    fireEvent.keyDown(handle, { key: "ArrowDown" });
    expect(onResize).toHaveBeenLastCalledWith(1, 400, 166);
    fireEvent.keyDown(handle, { key: "ArrowLeft" });
    expect(onResize).toHaveBeenLastCalledWith(1, 384, 150);
    fireEvent.keyDown(handle, { key: "ArrowUp" });
    expect(onResize).toHaveBeenLastCalledWith(1, 400, 134);
  });

  it("clamps a keyboard shrink to the minimum and ignores other keys", () => {
    const onResize = vi.fn();
    render(block({ onResize, width: 240, height: 96 }));
    const handle = screen.getByRole("button", { name: /resize/i });
    fireEvent.keyDown(handle, { key: "ArrowLeft" });
    expect(onResize).toHaveBeenLastCalledWith(1, 240, 96);
    onResize.mockClear();
    fireEvent.keyDown(handle, { key: "a" });
    expect(onResize).not.toHaveBeenCalled();
  });

  it("reorders when another block is dropped on it", () => {
    const onReorder = vi.fn();
    const { container } = render(block({ onReorder, index: 2 }));
    const el = container.firstElementChild as HTMLElement;
    expect(el).toHaveAttribute("draggable", "true");
    // Block 0 (another RadioBlock) published its index on dragstart.
    const dataTransfer = fakeDataTransfer({ "text/plain": "0" });
    fireEvent.drop(el, { dataTransfer });
    expect(onReorder).toHaveBeenCalledWith(0, 2);
  });

  it("publishes its own index when a drag starts, and ignores a drop on itself", () => {
    const onReorder = vi.fn();
    const { container } = render(block({ onReorder, index: 3 }));
    const el = container.firstElementChild as HTMLElement;
    const store: Record<string, string> = {};
    const dataTransfer = fakeDataTransfer(store);
    fireEvent.dragStart(el, { dataTransfer });
    expect(store["text/plain"]).toBe("3");
    fireEvent.drop(el, { dataTransfer });
    expect(onReorder).not.toHaveBeenCalled();
  });

  it("does not start a block drag from the resize handle", () => {
    const { container } = render(block());
    const handle = screen.getByRole("button", { name: /resize/i });
    fireEvent.pointerDown(handle, { clientX: 400, clientY: 150, pointerId: 1 });
    const el = container.firstElementChild as HTMLElement;
    const setData = vi.fn();
    const notCancelled = fireEvent.dragStart(el, { dataTransfer: { setData } });
    expect(notCancelled).toBe(false); // preventDefault was called
    expect(setData).not.toHaveBeenCalled();
    fireEvent.pointerUp(window, { pointerId: 1 });
  });

  it("detaches its window listeners on real unmount", () => {
    const remove = vi.spyOn(window, "removeEventListener");
    const { unmount } = render(block());
    const handle = screen.getByRole("button", { name: /resize/i });
    fireEvent.pointerDown(handle, { clientX: 400, clientY: 150, pointerId: 1 });
    remove.mockClear();
    unmount();
    expect(remove).toHaveBeenCalledWith("pointermove", expect.any(Function));
    expect(remove).toHaveBeenCalledWith("pointerup", expect.any(Function));
    remove.mockRestore();
  });

  it("survives StrictMode's simulated unmount with a working handle", () => {
    // The control for the above. StrictMode double-invokes effects; a
    // cleanup that tore down shared state permanently broke keybind capture
    // for two phases before it was caught.
    const onResize = vi.fn();
    render(<StrictMode>{block({ onResize })}</StrictMode>);
    const handle = screen.getByRole("button", { name: /resize/i });
    fireEvent.pointerDown(handle, { clientX: 400, clientY: 150, pointerId: 1 });
    fireEvent.pointerMove(window, { clientX: 500, clientY: 200, pointerId: 1 });
    fireEvent.pointerUp(window, { pointerId: 1 });
    expect(onResize).toHaveBeenCalledWith(1, 500, 200);
  });
});
