/* global React, Icon, LcdFreq, Knob, VU, Toggle, KeyChip */
/* ============================================================
   Radio widget — the signature component
   ============================================================ */
const { useState, useEffect, useRef } = React;

function RadioWidget({ radio, onChange, size = "md", selected, onSelect, onOpenText, onContext }) {
  const update = patch => onChange && onChange({ ...radio, ...patch });
  const [keyedDown, setKeyedDown] = useState(false);
  const keyed = radio.keyed || keyedDown;

  // simulate VU activity when a remote is talking on this freq
  const [rxLevel, setRxLevel] = useState(0);
  useEffect(() => {
    if (!radio.rxActive) { setRxLevel(0); return; }
    const t = setInterval(() => setRxLevel(0.3 + Math.random() * 0.6), 80);
    return () => clearInterval(t);
  }, [radio.rxActive]);

  // ptt key handler — hold radio.pttKey to key
  useEffect(() => {
    if (!radio.pttKey) return;
    const keyMatches = e => {
      const key = e.code === "Space" ? "Space" : e.key.length === 1 ? e.key.toUpperCase() : e.key;
      return key === radio.pttKey || `Ctrl+${key}` === radio.pttKey;
    };
    const down = e => {
      if (e.target.tagName === "INPUT" || e.target.tagName === "TEXTAREA") return;
      if (keyMatches(e)) { e.preventDefault(); setKeyedDown(true); }
    };
    const up = e => { if (keyMatches(e)) setKeyedDown(false); };
    window.addEventListener("keydown", down);
    window.addEventListener("keyup", up);
    return () => { window.removeEventListener("keydown", down); window.removeEventListener("keyup", up); };
  }, [radio.pttKey]);

  const big = size === "lg";
  const sm  = size === "sm";

  // Name editing
  const [editing, setEditing] = useState(false);
  const [draftName, setDraftName] = useState(radio.name);
  useEffect(() => setDraftName(radio.name), [radio.name]);

  const commitName = () => { setEditing(false); if (draftName.trim()) update({ name: draftName.trim() }); else setDraftName(radio.name); };

  return (
    <div
      className="radio"
      onClick={() => onSelect && onSelect(radio.id)}
      style={{
        position: "relative",
        background: "linear-gradient(180deg, var(--bg-3), var(--bg-2))",
        border: `1px solid ${selected ? "var(--ac-primary)" : "var(--bd-2)"}`,
        borderRadius: 6,
        padding: sm ? 10 : 14,
        display: "grid",
        gap: sm ? 8 : 12,
        gridTemplateColumns: "1fr",
        boxShadow: selected ? "0 0 0 1px var(--ac-primary-glow), inset 0 1px 0 rgba(255,255,255,0.03)" : "inset 0 1px 0 rgba(255,255,255,0.03)",
        cursor: "default",
        overflow: "hidden",
      }}
    >
      {/* corner brackets */}
      <span style={{ position: "absolute", top: -1, left: -1, width: 10, height: 10, borderTop: "1px solid var(--ac-primary)", borderLeft: "1px solid var(--ac-primary)", opacity: 0.7 }} />
      <span style={{ position: "absolute", top: -1, right: -1, width: 10, height: 10, borderTop: "1px solid var(--ac-primary)", borderRight: "1px solid var(--ac-primary)", opacity: 0.5 }} />
      <span style={{ position: "absolute", bottom: -1, left: -1, width: 10, height: 10, borderBottom: "1px solid var(--ac-primary)", borderLeft: "1px solid var(--ac-primary)", opacity: 0.5 }} />

      {/* header row: index + nickname + status + context menu */}
      <div className="row acenter gap-3" style={{ minHeight: 22 }}>
        <span className="cap mono" style={{ color: "var(--ac-primary)", letterSpacing: "0.16em" }}>R{String(radio.index).padStart(2, "0")}</span>
        {editing ? (
          <input
            autoFocus
            className="input"
            style={{ height: 22, fontSize: 12, padding: "0 6px", flex: 1 }}
            value={draftName}
            onChange={e => setDraftName(e.target.value)}
            onBlur={commitName}
            onKeyDown={e => e.key === "Enter" && commitName()}
          />
        ) : (
          <span
            className="flex"
            style={{ fontSize: 12, color: "var(--tx-0)", fontWeight: 500, letterSpacing: "0.04em", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", cursor: "text" }}
            onDoubleClick={() => setEditing(true)}
          >
            {radio.name}
          </span>
        )}
        {radio.role && <span className="cap" style={{ background: "var(--bg-1)", border: "1px solid var(--ac-primary-dim)", color: "var(--ac-primary)", padding: "1px 5px", borderRadius: 2, letterSpacing: "0.14em" }}>ROLE</span>}
        {radio.scanning && <span className="cap" style={{ color: "var(--ac-warn)" }}>SCAN</span>}
        {radio.muted && <Icon name="micOff" size={12} style={{ color: "var(--ac-warn)" }} />}
        <button className="btn btn-ghost btn-icon btn-sm" onClick={e => { e.stopPropagation(); onContext && onContext(radio.id); }} title="More">
          <Icon name="drag" size={12} />
        </button>
      </div>

      {/* LCD frequency display */}
      <div className="row acenter between gap-4">
        <LcdFreq freq={radio.freq} onChange={f => update({ freq: f })} size={big ? 36 : sm ? 22 : 26} />
        <div className="col gap-2" style={{ alignItems: "flex-end" }}>
          <div className="row gap-2">
            <span className={`pill ${radio.enc ? "" : "pill-disabled"}`} style={{ padding: "1px 6px", cursor: "pointer" }} onClick={e => { e.stopPropagation(); update({ enc: !radio.enc }); }}>
              <Icon name={radio.enc ? "lock" : "unlock"} size={9} />
              ENC {radio.enc ? `K${radio.encChan}` : "OFF"}
            </span>
            <span className="pill" style={{ padding: "1px 6px" }}>
              CH.{radio.channel}
            </span>
          </div>
        </div>
      </div>

      {/* Controls row: knobs + PTT */}
      <div className="row acenter gap-5" style={{ justifyContent: "space-between" }}>
        <div className="row acenter gap-6" style={{ paddingBottom: 14 }}>
          <Knob value={radio.volume} onChange={v => update({ volume: v })} label="VOL" size={sm ? 32 : 40} />
          <Knob value={radio.balance} onChange={v => update({ balance: v })} label="BAL L/R" min={-50} max={50} size={sm ? 26 : 32} center />
        </div>
        <button
          className={`ptt ${keyed ? "keyed" : ""} ${radio.rxActive ? "ptt-rx" : ""}`}
          style={{ flex: 1, maxWidth: big ? 220 : 160, height: big ? 64 : 48 }}
          onMouseDown={e => { e.stopPropagation(); setKeyedDown(true); }}
          onMouseUp={() => setKeyedDown(false)}
          onMouseLeave={() => setKeyedDown(false)}
          title="Push to Talk"
        >
          {radio.rxActive ? "RECEIVING" : keyed ? "TRANSMIT" : "PUSH-TO-TALK"}
        </button>
      </div>

      {/* Bottom row: status + keybinds */}
      <div className="row acenter between gap-3" style={{ borderTop: "1px solid var(--bd-1)", paddingTop: 8, fontFamily: "var(--ff-mono)", fontSize: 10, color: "var(--tx-3)" }}>
        <div className="row acenter gap-3 flex" style={{ overflow: "hidden" }}>
          <span style={{
            width: 6, height: 6, borderRadius: "50%",
            background: radio.rxActive ? "var(--ac-ok)" : radio.tuned > 0 ? "var(--ac-primary)" : "var(--tx-4)",
            boxShadow: radio.rxActive ? "0 0 6px var(--ac-ok)" : radio.tuned > 0 ? "0 0 4px var(--ac-primary)" : "none",
            flex: "0 0 6px",
          }}/>
          <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", color: radio.rxActive ? "var(--ac-ok)" : "var(--tx-2)" }}>
            {radio.rxActive ? `▶ ${radio.lastTalker}` : radio.lastTalker ? `last: ${radio.lastTalker}` : "no traffic"}
          </span>
          <VU level={keyed ? 0.6 + Math.random() * 0.3 : rxLevel} segs={6} />
        </div>
        <div className="row acenter gap-3">
          <button
            className="btn-ghost"
            title="Text channel"
            onClick={e => { e.stopPropagation(); onOpenText && onOpenText(radio.id); }}
            style={{ display: "inline-flex", alignItems: "center", gap: 4, color: radio.unread ? "var(--ac-warn)" : "var(--tx-3)", cursor: "pointer" }}
          >
            <Icon name="chat" size={12} />
            {radio.unread > 0 && <span style={{ fontSize: 9, color: "#fff", background: "var(--ac-alert)", padding: "0 4px", borderRadius: 999 }}>{radio.unread}</span>}
          </button>
          <span className="sep-v" style={{ height: 12 }}/>
          <span className="row acenter gap-2" title="PTT keybind">
            <span style={{ color: "var(--tx-4)" }}>PTT</span>
            <KeyChip binding={radio.pttKey} onRebind={k => update({ pttKey: k })} />
          </span>
          <span className="row acenter gap-2" title="Select keybind">
            <span style={{ color: "var(--tx-4)" }}>SEL</span>
            <KeyChip binding={radio.selectKey} onRebind={k => update({ selectKey: k })} />
          </span>
        </div>
      </div>
    </div>
  );
}

/* ---------- Compact strip radio (for overlay or compact layouts) ---------- */
function RadioStrip({ radio, onChange }) {
  const [keyed, setKeyed] = useState(false);
  const update = p => onChange && onChange({ ...radio, ...p });
  return (
    <div className="row acenter gap-4" style={{
      background: "linear-gradient(180deg, rgba(10,20,32,0.95), rgba(6,13,22,0.95))",
      border: "1px solid var(--bd-2)",
      borderRadius: 6,
      padding: "8px 12px",
      height: 120,
      width: 360,
    }}>
      <div className="col gap-2" style={{ flex: 1, minWidth: 0 }}>
        <div className="row acenter gap-3">
          <span className="cap mono" style={{ color: "var(--ac-primary)" }}>R{String(radio.index).padStart(2, "0")}</span>
          <span style={{ fontSize: 11, color: "var(--tx-0)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{radio.name}</span>
        </div>
        <LcdFreq freq={radio.freq} onChange={f => update({ freq: f })} size={20} />
        <div className="row acenter gap-3" style={{ fontFamily: "var(--ff-mono)", fontSize: 9, color: "var(--tx-3)" }}>
          <span style={{ width: 5, height: 5, borderRadius: "50%", background: radio.rxActive ? "var(--ac-ok)" : "var(--tx-4)" }}/>
          <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{radio.rxActive ? radio.lastTalker : "—"}</span>
          <VU level={keyed ? 0.7 : 0} segs={5} />
        </div>
      </div>
      <button
        className={`ptt ${keyed ? "keyed" : ""}`}
        style={{ width: 80, height: 80, fontSize: 9, padding: 0 }}
        onMouseDown={() => setKeyed(true)}
        onMouseUp={() => setKeyed(false)}
        onMouseLeave={() => setKeyed(false)}
      >
        PTT
      </button>
    </div>
  );
}

Object.assign(window, { RadioWidget, RadioStrip });
