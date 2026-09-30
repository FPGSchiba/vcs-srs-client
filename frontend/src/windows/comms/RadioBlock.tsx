import { useEffect, useRef, useState } from "react";
import type { DragEvent, KeyboardEvent, PointerEvent } from "react";
import type { RadioDTO } from "../../shared/api/client";
import { clampBlock } from "../../shared/layout";
import { RadioCard } from "./RadioCard";

interface Props {
  radio: RadioDTO;
  allRadios: RadioDTO[];
  muted: boolean;
  width: number;
  height: number;
  index: number;
  onResize: (radioId: number, w: number, h: number) => void;
  onReorder: (from: number, to: number) => void;
}

/** Keyboard nudge for the resize handle, in px per arrow press. */
const KEY_STEP = 16;
const DRAG_MIME = "text/plain";

interface DragStart {
  x: number;
  y: number;
  w: number;
  h: number;
}

/**
 * RadioBlock wraps a RadioCard in a block of explicit pixel size with a
 * bottom-right resize handle. The size is fully controlled: this component
 * never stores it, it reports `onResize(radioId, w, h)` and the parent (which
 * owns the layout and debounces persistence) feeds the new size back in.
 *
 * Resize drags attach `pointermove`/`pointerup` to `window` for exactly as long
 * as a drag is active (effect keyed on the drag origin). The cleanup only
 * removes the two listeners this effect added, so running it twice -- StrictMode's
 * simulated unmount, then the real one -- is harmless and leaves no shared
 * state torn down. The handle is a span with pointer handlers, so it carries
 * role="button", tabIndex and arrow-key nudging (typescript:S1082).
 *
 * Reordering uses HTML5 drag-and-drop on the block body. The drag payload is
 * the source index; `onReorder(from, to)` fires on the target block. There is
 * deliberately no keyboard path: RESET restores the stored order.
 */
export function RadioBlock({
  radio,
  allRadios,
  muted,
  width,
  height,
  index,
  onResize,
  onReorder,
}: Props) {
  const [drag, setDrag] = useState<DragStart | null>(null);

  // Latest props for the window listeners, so they are attached once per drag
  // rather than re-attached on every reported size.
  const latest = useRef({ radioId: radio.id, onResize });
  latest.current = { radioId: radio.id, onResize };
  // The last size reported during this drag, so pointerup can commit it.
  const lastSize = useRef<{ w: number; h: number } | null>(null);

  useEffect(() => {
    if (!drag) return;
    const move = (e: globalThis.PointerEvent) => {
      const { w, h } = clampBlock(drag.w + (e.clientX - drag.x), drag.h + (e.clientY - drag.y));
      lastSize.current = { w, h };
      latest.current.onResize(latest.current.radioId, w, h);
    };
    const up = () => {
      const last = lastSize.current;
      if (last) latest.current.onResize(latest.current.radioId, last.w, last.h);
      lastSize.current = null;
      setDrag(null);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
    return () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
    };
  }, [drag]);

  function startResize(e: PointerEvent<HTMLSpanElement>) {
    e.preventDefault();
    e.stopPropagation();
    try {
      e.currentTarget.setPointerCapture?.(e.pointerId);
    } catch {
      /* capture is a nicety; window listeners still deliver the drag */
    }
    lastSize.current = null;
    setDrag({ x: e.clientX, y: e.clientY, w: width, h: height });
  }

  function nudge(e: KeyboardEvent<HTMLSpanElement>) {
    let dw = 0;
    let dh = 0;
    switch (e.key) {
      case "ArrowRight":
        dw = KEY_STEP;
        break;
      case "ArrowLeft":
        dw = -KEY_STEP;
        break;
      case "ArrowDown":
        dh = KEY_STEP;
        break;
      case "ArrowUp":
        dh = -KEY_STEP;
        break;
      default:
        return;
    }
    e.preventDefault();
    const { w, h } = clampBlock(width + dw, height + dh);
    onResize(radio.id, w, h);
  }

  function onDragStart(e: DragEvent<HTMLDivElement>) {
    // A resize drag starts with a pointerdown inside this draggable element;
    // the browser would otherwise also begin an HTML5 drag of the whole block.
    if (drag) {
      e.preventDefault();
      return;
    }
    e.dataTransfer.setData(DRAG_MIME, String(index));
    e.dataTransfer.effectAllowed = "move";
  }

  function onDragOver(e: DragEvent<HTMLDivElement>) {
    e.preventDefault();
    e.dataTransfer.dropEffect = "move";
  }

  function onDrop(e: DragEvent<HTMLDivElement>) {
    e.preventDefault();
    const from = Number.parseInt(e.dataTransfer.getData(DRAG_MIME), 10);
    if (Number.isNaN(from) || from === index) return;
    onReorder(from, index);
  }

  return (
    <div
      draggable
      onDragStart={onDragStart}
      onDragOver={onDragOver}
      onDrop={onDrop}
      style={{
        position: "relative",
        width,
        height,
        flexShrink: 0,
        boxSizing: "border-box",
      }}
    >
      {/* RadioCard keeps its natural height; a block shorter than the card
          scrolls rather than clipping the controls out of reach. */}
      <div style={{ width: "100%", height: "100%", overflow: "auto" }}>
        <RadioCard radio={radio} allRadios={allRadios} muted={muted} />
      </div>
      <span
        role="button"
        tabIndex={0}
        aria-label="Resize radio block"
        title="Drag to resize (arrow keys nudge)"
        onPointerDown={startResize}
        onKeyDown={nudge}
        style={{
          position: "absolute",
          right: 0,
          bottom: 0,
          width: 16,
          height: 16,
          cursor: "nwse-resize",
          touchAction: "none",
          borderRight: "2px solid var(--ac-primary)",
          borderBottom: "2px solid var(--ac-primary)",
        }}
      />
    </div>
  );
}
