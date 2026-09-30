import { useEffect, useRef, useState } from "react";
import type { DragEvent, KeyboardEvent, PointerEvent } from "react";
import type { RadioDTO } from "../../shared/api/client";
import { VARIANTS, nearestVariant, variantById } from "./variants";
import { RadioCard } from "./RadioCard";

interface Props {
  radio: RadioDTO;
  allRadios: RadioDTO[];
  muted: boolean;
  variantId: string;
  index: number;
  onResize: (radioId: number, variantId: string) => void;
  onReorder: (from: number, to: number) => void;
}

const DRAG_MIME = "text/plain";

/**
 * Variants smallest-first, so an arrow key means "one size up/down". Areas can
 * tie (narrow-h and narrow-v are both 20400), so the narrower one sorts first.
 */
const BY_AREA = [...VARIANTS].sort(
  (a, b) => a.w * a.h - b.w * b.h || a.w - b.w || a.id.localeCompare(b.id),
);

interface DragStart {
  x: number;
  y: number;
  w: number;
  h: number;
}

/**
 * RadioBlock wraps a RadioCard and resizes it by SNAPPING to a variant.
 *
 * The drag reports nothing while the pointer is down: it previews locally and
 * commits exactly one variant on pointerup. That is what makes it snappy — no
 * IPC round trip per pointermove frame, and the card never renders at an
 * in-between size, because there is no such size.
 *
 * Window listeners are attached for exactly as long as a drag is active
 * (effect keyed on the drag origin) and the cleanup removes only the two this
 * effect added, so StrictMode's simulated unmount followed by the real one is
 * harmless.
 *
 * The handle sits on the card's own bottom-right corner, inside the block —
 * the block IS the card's box now, so there is no gap for it to float in.
 *
 * Reordering uses HTML5 drag-and-drop on the block body; the payload is the
 * source index and `onReorder(from, to)` fires on the target. There is
 * deliberately no keyboard path: RESET restores the stored order.
 */
export function RadioBlock({
  radio,
  allRadios,
  muted,
  variantId,
  index,
  onResize,
  onReorder,
}: Props) {
  const [drag, setDrag] = useState<DragStart | null>(null);
  const [previewId, setPreviewId] = useState<string | null>(null);

  const variant = variantById(variantId);

  // Latest values for the window listeners, so they attach once per drag.
  const latest = useRef({ radioId: radio.id, from: variant.id, onResize });
  latest.current = { radioId: radio.id, from: variant.id, onResize };
  const landed = useRef<string | null>(null);

  useEffect(() => {
    if (!drag) return;
    const move = (e: globalThis.PointerEvent) => {
      const v = nearestVariant(drag.w + (e.clientX - drag.x), drag.h + (e.clientY - drag.y));
      landed.current = v.id;
      setPreviewId(v.id);
    };
    const up = () => {
      const to = landed.current;
      landed.current = null;
      setPreviewId(null);
      setDrag(null);
      // A drag that ends on the variant it started from is not a change.
      if (to && to !== latest.current.from) latest.current.onResize(latest.current.radioId, to);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
    return () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
    };
  }, [drag]);

  function startResize(e: PointerEvent<HTMLButtonElement>) {
    e.preventDefault();
    e.stopPropagation();
    try {
      e.currentTarget.setPointerCapture?.(e.pointerId);
    } catch {
      /* capture is a nicety; the window listeners still deliver the drag */
    }
    landed.current = null;
    setDrag({ x: e.clientX, y: e.clientY, w: variant.w, h: variant.h });
  }

  function nudge(e: KeyboardEvent<HTMLButtonElement>) {
    let step = 0;
    // Enter / Space activate the button like ArrowRight: step to the next size
    // up (a button must answer both). The ends do not wrap.
    if (e.key === "ArrowRight" || e.key === "ArrowDown" || e.key === "Enter" || e.key === " ")
      step = 1;
    else if (e.key === "ArrowLeft" || e.key === "ArrowUp") step = -1;
    else return;
    e.preventDefault();
    const i = BY_AREA.findIndex((v) => v.id === variant.id);
    const next = BY_AREA[i + step];
    if (next) onResize(radio.id, next.id); // the ends do not wrap
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

  function onDrop(e: DragEvent<HTMLDivElement>) {
    e.preventDefault();
    const from = Number.parseInt(e.dataTransfer.getData(DRAG_MIME), 10);
    if (Number.isNaN(from) || from === index) return;
    onReorder(from, index);
  }

  const preview = previewId ? variantById(previewId) : null;

  return (
    <div
      data-testid="radio-block"
      draggable
      onDragStart={onDragStart}
      onDragOver={(e) => {
        e.preventDefault();
        e.dataTransfer.dropEffect = "move";
      }}
      onDrop={onDrop}
      style={{
        position: "relative",
        width: variant.w,
        height: variant.h,
        flexShrink: 0,
        boxSizing: "border-box",
      }}
    >
      <RadioCard
        radio={radio}
        allRadios={allRadios}
        muted={muted}
        variantId={variant.id}
        onVariantChange={(id) => onResize(radio.id, id)}
      />
      {preview && (
        <span
          data-testid="resize-preview"
          style={{
            position: "absolute",
            left: 0,
            top: 0,
            width: preview.w,
            height: preview.h,
            pointerEvents: "none",
            border: "1px dashed var(--ac-primary)",
            background: "color-mix(in srgb, var(--ac-primary) 8%, transparent)",
            color: "var(--ac-primary)",
            fontSize: 10,
            letterSpacing: "0.14em",
            textTransform: "uppercase",
            padding: 4,
            zIndex: 2,
          }}
        >
          {preview.label}
        </span>
      )}
      <button
        type="button"
        aria-label="Resize radio block"
        title="Drag to snap to a size (arrow keys, Enter or Space step through sizes)"
        onPointerDown={startResize}
        onKeyDown={nudge}
        style={{
          position: "absolute",
          right: 0,
          bottom: 0,
          width: 14,
          height: 14,
          cursor: "nwse-resize",
          touchAction: "none",
          zIndex: 3,
          borderRight: "2px solid var(--ac-primary)",
          borderBottom: "2px solid var(--ac-primary)",
        }}
      />
    </div>
  );
}
