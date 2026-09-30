import { render, screen, fireEvent } from "@testing-library/react";
import { StrictMode } from "react";
import { describe, expect, it, vi } from "vitest";
import { RadioBlock } from "./RadioBlock";

const radio = { id: 1, name: "Fleet Common", frequency: 118.5, enabled: true, is_intercom: false };

function renderBlock(p: {
  variantId: string;
  onResize: (id: number, v: string) => void;
  onReorder?: (from: number, to: number) => void;
  index?: number;
  strict?: boolean;
}) {
  const el = (
    <RadioBlock
      radio={radio as never}
      allRadios={[radio] as never}
      muted={false}
      variantId={p.variantId}
      index={p.index ?? 0}
      onResize={p.onResize}
      onReorder={p.onReorder ?? vi.fn()}
    />
  );
  return render(p.strict ? <StrictMode>{el}</StrictMode> : el);
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

describe("snap resizing", () => {
  it("reports nothing while the pointer is still down", () => {
    const onResize = vi.fn();
    renderBlock({ variantId: "vertical", onResize });
    const handle = screen.getByRole("button", { name: /resize/i });
    fireEvent.pointerDown(handle, { clientX: 0, clientY: 0 });
    fireEvent.pointerMove(window, { clientX: 80, clientY: -60 });
    expect(onResize).not.toHaveBeenCalled();
  });

  it("previews the variant the current drag would land on", () => {
    renderBlock({ variantId: "vertical", onResize: vi.fn() });
    const handle = screen.getByRole("button", { name: /resize/i });
    fireEvent.pointerDown(handle, { clientX: 0, clientY: 0 });
    // 280x166 dragged by (+80, -56) -> ~360x110 -> horizontal.
    fireEvent.pointerMove(window, { clientX: 80, clientY: -56 });
    expect(screen.getByTestId("resize-preview")).toHaveTextContent("Horizontal");
  });

  it("commits exactly one variant on pointerup", () => {
    const onResize = vi.fn();
    renderBlock({ variantId: "vertical", onResize });
    const handle = screen.getByRole("button", { name: /resize/i });
    fireEvent.pointerDown(handle, { clientX: 0, clientY: 0 });
    fireEvent.pointerMove(window, { clientX: 80, clientY: -56 });
    fireEvent.pointerMove(window, { clientX: 81, clientY: -56 });
    fireEvent.pointerUp(window);
    expect(onResize).toHaveBeenCalledTimes(1);
    expect(onResize).toHaveBeenCalledWith(1, "horizontal");
  });

  it("clears the preview when the drag ends", () => {
    renderBlock({ variantId: "vertical", onResize: vi.fn() });
    const handle = screen.getByRole("button", { name: /resize/i });
    fireEvent.pointerDown(handle, { clientX: 0, clientY: 0 });
    fireEvent.pointerMove(window, { clientX: 80, clientY: -56 });
    fireEvent.pointerUp(window);
    expect(screen.queryByTestId("resize-preview")).toBeNull();
  });

  it("reports nothing when the drag lands back on the current variant", () => {
    const onResize = vi.fn();
    renderBlock({ variantId: "vertical", onResize });
    const handle = screen.getByRole("button", { name: /resize/i });
    fireEvent.pointerDown(handle, { clientX: 0, clientY: 0 });
    fireEvent.pointerMove(window, { clientX: 3, clientY: 2 });
    fireEvent.pointerUp(window);
    expect(onResize).not.toHaveBeenCalled();
  });

  it("measures the drag from where the pointer went down, not from screen zero", () => {
    // Origin (400,150): move to (270,108) is delta (-130,-42), so
    // 280-130=150 by 166-42=124 -> narrow-v exactly. Without subtracting the
    // origin the size would be 550x274, which is nowhere near narrow-v.
    const onResize = vi.fn();
    renderBlock({ variantId: "vertical", onResize });
    const handle = screen.getByRole("button", { name: /resize/i });
    fireEvent.pointerDown(handle, { clientX: 400, clientY: 150 });
    fireEvent.pointerMove(window, { clientX: 270, clientY: 108 });
    fireEvent.pointerUp(window);
    expect(onResize).toHaveBeenCalledTimes(1);
    expect(onResize).toHaveBeenCalledWith(1, "narrow-v");
  });

  it("a tiny drag from a non-zero origin is still no change", () => {
    // Delta (+3,+2) -> 283x168 -> vertical. Without subtracting the origin it
    // would be 683x318 and commit a different variant.
    const onResize = vi.fn();
    renderBlock({ variantId: "vertical", onResize });
    const handle = screen.getByRole("button", { name: /resize/i });
    fireEvent.pointerDown(handle, { clientX: 400, clientY: 150 });
    fireEvent.pointerMove(window, { clientX: 403, clientY: 152 });
    fireEvent.pointerUp(window);
    expect(onResize).not.toHaveBeenCalled();
  });

  it("sits the handle on the card's own corner, inside the block", () => {
    renderBlock({ variantId: "vertical", onResize: vi.fn() });
    const handle = screen.getByRole("button", { name: /resize/i });
    expect(handle).toHaveStyle({ position: "absolute", right: "0px", bottom: "0px" });
    // Inside the block's bounds, which are the card's bounds exactly.
    expect(screen.getByTestId("radio-block")).toHaveStyle({ width: "280px", height: "168px" });
  });

  it("stops resizing after pointer up", () => {
    const onResize = vi.fn();
    renderBlock({ variantId: "vertical", onResize });
    const handle = screen.getByRole("button", { name: /resize/i });
    fireEvent.pointerDown(handle, { clientX: 0, clientY: 0 });
    fireEvent.pointerMove(window, { clientX: 80, clientY: -56 });
    fireEvent.pointerUp(window);
    onResize.mockClear();
    fireEvent.pointerMove(window, { clientX: 200, clientY: 200 });
    fireEvent.pointerUp(window);
    expect(onResize).not.toHaveBeenCalled();
    expect(screen.queryByTestId("resize-preview")).toBeNull();
  });

  it("does not resize on pointer movement that never started on the handle", () => {
    const onResize = vi.fn();
    renderBlock({ variantId: "vertical", onResize });
    fireEvent.pointerMove(window, { clientX: 80, clientY: -56 });
    fireEvent.pointerUp(window);
    expect(onResize).not.toHaveBeenCalled();
    expect(screen.queryByTestId("resize-preview")).toBeNull();
  });
});

describe("keyboard resizing", () => {
  it("steps to the next larger variant and the next smaller", () => {
    const onResize = vi.fn();
    renderBlock({ variantId: "narrow-h", onResize });
    const handle = screen.getByRole("button", { name: /resize/i });
    expect(handle.tagName).toBe("BUTTON"); // natively focusable, in the tab order
    expect(handle.tabIndex).toBe(0);
    // Variants ordered by area, narrower first on a tie: narrow-v (150x136) and
    // narrow-h (300x68) are both 20400, then horizontal (41400), vertical (47040).
    fireEvent.keyDown(handle, { key: "ArrowRight" });
    expect(onResize).toHaveBeenLastCalledWith(1, "horizontal");
    fireEvent.keyDown(handle, { key: "ArrowLeft" });
    expect(onResize).toHaveBeenLastCalledWith(1, "narrow-v");
  });

  it("Enter and Space activate the handle like ArrowRight: step to the next size", () => {
    const onResize = vi.fn();
    renderBlock({ variantId: "horizontal", onResize });
    const handle = screen.getByRole("button", { name: /resize/i });
    fireEvent.keyDown(handle, { key: "Enter" });
    expect(onResize).toHaveBeenLastCalledWith(1, "vertical");
    fireEvent.keyDown(handle, { key: " " });
    expect(onResize).toHaveBeenCalledTimes(2);
    expect(onResize).toHaveBeenLastCalledWith(1, "vertical");
  });

  it("stops at the ends rather than wrapping", () => {
    const onResize = vi.fn();
    renderBlock({ variantId: "narrow-v", onResize });
    fireEvent.keyDown(screen.getByRole("button", { name: /resize/i }), { key: "ArrowLeft" });
    expect(onResize).not.toHaveBeenCalled();
  });

  it("does not wrap at the top end", () => {
    const onResize = vi.fn();
    renderBlock({ variantId: "vertical", onResize });
    const handle = screen.getByRole("button", { name: /resize/i });
    fireEvent.keyDown(handle, { key: "ArrowRight" });
    fireEvent.keyDown(handle, { key: "ArrowDown" });
    expect(onResize).not.toHaveBeenCalled();
  });

  it("ArrowDown steps up and ArrowUp steps down, like Right and Left", () => {
    const onResize = vi.fn();
    renderBlock({ variantId: "horizontal", onResize });
    const handle = screen.getByRole("button", { name: /resize/i });
    fireEvent.keyDown(handle, { key: "ArrowDown" });
    expect(onResize).toHaveBeenLastCalledWith(1, "vertical");
    fireEvent.keyDown(handle, { key: "ArrowUp" });
    expect(onResize).toHaveBeenLastCalledWith(1, "narrow-h");
  });

  it("ArrowUp does not wrap below the smallest", () => {
    const onResize = vi.fn();
    renderBlock({ variantId: "narrow-v", onResize });
    fireEvent.keyDown(screen.getByRole("button", { name: /resize/i }), { key: "ArrowUp" });
    expect(onResize).not.toHaveBeenCalled();
  });

  it("ignores other keys", () => {
    const onResize = vi.fn();
    renderBlock({ variantId: "narrow-h", onResize });
    fireEvent.keyDown(screen.getByRole("button", { name: /resize/i }), { key: "a" });
    expect(onResize).not.toHaveBeenCalled();
  });
});

describe("reordering and lifecycle", () => {
  it("reorders when another block is dropped on it", () => {
    const onReorder = vi.fn();
    renderBlock({ variantId: "vertical", onResize: vi.fn(), onReorder, index: 2 });
    const el = screen.getByTestId("radio-block");
    expect(el).toHaveAttribute("draggable", "true");
    // Block 0 (another RadioBlock) published its index on dragstart.
    fireEvent.drop(el, { dataTransfer: fakeDataTransfer({ "text/plain": "0" }) });
    expect(onReorder).toHaveBeenCalledWith(0, 2);
  });

  it("publishes its own index when a drag starts, and ignores a drop on itself", () => {
    const onReorder = vi.fn();
    renderBlock({ variantId: "vertical", onResize: vi.fn(), onReorder, index: 3 });
    const el = screen.getByTestId("radio-block");
    const store: Record<string, string> = {};
    const dataTransfer = fakeDataTransfer(store);
    fireEvent.dragStart(el, { dataTransfer });
    expect(store["text/plain"]).toBe("3");
    fireEvent.drop(el, { dataTransfer });
    expect(onReorder).not.toHaveBeenCalled();
  });

  it("does not start a block drag from the resize handle", () => {
    renderBlock({ variantId: "vertical", onResize: vi.fn() });
    const handle = screen.getByRole("button", { name: /resize/i });
    fireEvent.pointerDown(handle, { clientX: 0, clientY: 0, pointerId: 1 });
    const setData = vi.fn();
    const notCancelled = fireEvent.dragStart(screen.getByTestId("radio-block"), {
      dataTransfer: { setData },
    });
    expect(notCancelled).toBe(false); // preventDefault was called
    expect(setData).not.toHaveBeenCalled();
    fireEvent.pointerUp(window, { pointerId: 1 });
  });

  it("detaches its window listeners on real unmount", () => {
    const remove = vi.spyOn(window, "removeEventListener");
    const { unmount } = renderBlock({ variantId: "vertical", onResize: vi.fn() });
    const handle = screen.getByRole("button", { name: /resize/i });
    fireEvent.pointerDown(handle, { clientX: 0, clientY: 0, pointerId: 1 });
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
    renderBlock({ variantId: "vertical", onResize, strict: true });
    const handle = screen.getByRole("button", { name: /resize/i });
    fireEvent.pointerDown(handle, { clientX: 0, clientY: 0, pointerId: 1 });
    fireEvent.pointerMove(window, { clientX: 80, clientY: -56, pointerId: 1 });
    fireEvent.pointerUp(window, { pointerId: 1 });
    expect(onResize).toHaveBeenCalledTimes(1);
    expect(onResize).toHaveBeenCalledWith(1, "horizontal");
  });
});
