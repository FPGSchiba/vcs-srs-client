/* global React, Icon */
/* ============================================================
   PopOut window — independent floating window on the desktop
   ============================================================ */
const { useState: usePopState, useRef: usePopRef, useEffect: usePopEffect } = React;

function PopOut({ id, title, meta, x, y, w, h, minW = 360, minH = 240, active, onFocus, onMove, onResize, onClose, onDock, onMinimize, children }) {
  const dragRef = usePopRef(null);

  const onDragStart = e => {
    if (e.target.closest("button")) return;
    e.preventDefault();
    onFocus && onFocus();
    const startX = e.clientX, startY = e.clientY;
    const x0 = x, y0 = y;
    const stage = document.querySelector(".desktop");
    const scale = stage ? parseFloat(stage.dataset.scale || 1) : 1;
    const move = ev => onMove({ x: x0 + (ev.clientX - startX) / scale, y: y0 + (ev.clientY - startY) / scale });
    const up = () => { window.removeEventListener("pointermove", move); window.removeEventListener("pointerup", up); };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  };

  const onResizeStart = e => {
    e.preventDefault();
    e.stopPropagation();
    onFocus && onFocus();
    const startX = e.clientX, startY = e.clientY;
    const w0 = w, h0 = h;
    const stage = document.querySelector(".desktop");
    const scale = stage ? parseFloat(stage.dataset.scale || 1) : 1;
    const move = ev => {
      onResize({
        w: Math.max(minW, w0 + (ev.clientX - startX) / scale),
        h: Math.max(minH, h0 + (ev.clientY - startY) / scale),
      });
    };
    const up = () => { window.removeEventListener("pointermove", move); window.removeEventListener("pointerup", up); };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  };

  return (
    <div
      className={`popout ${active ? "active" : ""}`}
      style={{ left: x, top: y, width: w, height: h }}
      onMouseDown={onFocus}
    >
      <div className="popout-chrome" onPointerDown={onDragStart} ref={dragRef}>
        <div className="row acenter gap-3">
          <Icon name="broadcast" size={11} style={{ color: "var(--ac-primary)" }} />
          <span className="ttl">{title}</span>
        </div>
        <span className="meta">{meta}</span>
        <div className="ctrl">
          {onDock && (
            <button title="Dock to main" onClick={onDock}>
              <Icon name="pin" size={12} />
            </button>
          )}
          {onMinimize && (
            <button title="Minimize" onClick={onMinimize}>
              <Icon name="minimize" size={11} />
            </button>
          )}
          <button title="Maximize" onClick={() => onResize({ w: 1200, h: 760 })}>
            <Icon name="maximize" size={10} />
          </button>
          <button className="close" title="Close" onClick={onClose}>
            <Icon name="close" size={11} />
          </button>
        </div>
      </div>
      <div className="popout-body">{children}</div>
      <div className="popout-resize" onPointerDown={onResizeStart} />
    </div>
  );
}

/* Default geometries per pop-out */
const POPOUT_DEFAULTS = {
  comms: {
    title: "Communications",
    meta: "FLEET COMMS · 6 RADIOS",
    x: 1200, y: 60, w: 540, h: 720,
    minW: 480, minH: 480,
  },
  fleet: {
    title: "Fleet Mode",
    meta: "C2 · OP STARWALK · LIVE",
    x: 40, y: 60, w: 1300, h: 880,
    minW: 1000, minH: 700,
  },
  ship: {
    title: "Ship Mode",
    meta: "ARK-04 PERSEPHONE · ENGINEERING",
    x: 40, y: 60, w: 1120, h: 760,
    minW: 800, minH: 560,
  },
  messages: {
    title: "Messages",
    meta: "TEXT CHANNELS · 6 MIRRORS",
    x: 80, y: 80, w: 980, h: 700,
    minW: 640, minH: 420,
  },
  notifications: {
    title: "Notifications",
    meta: "12 UNREAD · 7 CATEGORIES",
    x: 1240, y: 120, w: 460, h: 600,
    minW: 380, minH: 360,
  },
};

Object.assign(window, { PopOut, POPOUT_DEFAULTS });
