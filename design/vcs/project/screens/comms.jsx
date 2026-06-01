/* global React, Icon, LcdFreq, Knob, VU, HealthBar, Toggle, KeyChip, StatusPill, Tac, Panel, StateCard, Segmented, RadioWidget, RadioStrip */
/* ============================================================
   Communications screen — the main working surface
   ============================================================ */
const { useState: useCommsState, useEffect: useCommsEffect, useMemo: useCommsMemo, useRef: useCommsRef } = React;

function ScreenComms({ app, setApp }) {
  const [editLayout, setEditLayout] = useCommsState(false);
  const [drawerOpen, setDrawerOpen] = useCommsState(true);
  const [drawerTab, setDrawerTab] = useCommsState("recent");
  const radios = app.radios;
  const compact = app.layoutPreset === "compact";

  const setRadios = next => setApp({ radios: typeof next === "function" ? next(app.radios) : next, profile: { ...app.profile, dirty: true } });

  const update = (id, patch) => setRadios(r => r.map(x => x.id === id ? { ...x, ...patch } : x));
  const addRadio = () => {
    setRadios(r => [...r, makeRadio(r.length + 1, { freq: 122.000, name: `Radio ${r.length + 1}` })]);
  };
  const removeRadio = id => setRadios(r => r.filter(x => x.id !== id));

  return (
    <div style={{ height: "100%", display: "grid", gridTemplateColumns: drawerOpen ? "1fr 280px" : "1fr 0", minHeight: 0 }}>
      <div style={{ display: "flex", flexDirection: "column", minHeight: 0 }}>
        {/* Toolbar */}
        <div style={{ padding: "10px 14px", borderBottom: "1px solid var(--bd-1)", background: "var(--bg-0)", display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap" }}>
          <button className="btn btn-primary" onClick={addRadio}><Icon name="plus" size={12}/> ADD RADIO</button>
          <div className="row acenter gap-3" style={{ padding: "0 6px", border: "1px solid var(--bd-2)", borderRadius: 4, background: "var(--bg-2)", height: 28 }}>
            <span className="cap">PROFILE</span>
            <select
              value={app.profile.id}
              onChange={e => setApp({ profile: { ...app.profile, id: e.target.value, name: e.target.options[e.target.selectedIndex].text, dirty: false } })}
              style={{ border: "none", background: "transparent", color: "var(--tx-0)", fontSize: 11, fontFamily: "var(--ff-mono)", outline: "none" }}
            >
              {app.profiles.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
            </select>
            {app.profile.dirty && <span className="cap" style={{ color: "var(--ac-warn)" }}>● UNSAVED</span>}
          </div>
          <button className="btn"><Icon name="save" size={11}/> SAVE</button>
          <button className="btn" onClick={() => setApp({ view: "profiles" })}><Icon name="folder" size={11}/> MANAGE</button>
          <span className="sep-v" style={{ height: 24 }}/>
          <span className="cap">LAYOUT</span>
          <Segmented
            value={app.layoutPreset}
            options={[
              { value: "default", label: "2×2", icon: "grid" },
              { value: "power", label: "POWER", icon: "layout" },
              { value: "compact", label: "STRIP", icon: "list" },
            ]}
            onChange={v => setApp({ layoutPreset: v })}
          />
          <button className={`btn ${editLayout ? "btn-primary" : ""}`} onClick={() => setEditLayout(e => !e)}>
            <Icon name="edit" size={11}/> EDIT LAYOUT
          </button>
          <button className="btn" onClick={() => setApp({ overlayMode: true })}><Icon name="pin" size={11}/> COMPACT OVERLAY</button>
          <div className="flex"/>
          <button className="btn btn-ghost btn-icon" onClick={() => setDrawerOpen(d => !d)} title="Toggle drawer">
            <Icon name={drawerOpen ? "chevron" : "chevronD"} size={14} />
          </button>
        </div>

        {/* Selected radio strip */}
        <div style={{ padding: "8px 14px", background: "var(--bg-0)", borderBottom: "1px solid var(--bd-1)", display: "flex", alignItems: "center", gap: 12 }}>
          <span className="cap">SELECTED</span>
          <span style={{ fontSize: 12, color: "var(--ac-primary)", fontFamily: "var(--ff-mono)", letterSpacing: "0.08em" }}>
            {(() => { const r = radios.find(r => r.id === app.selectedRadioId); return r ? `R${String(r.index).padStart(2,"0")} · ${r.name} · ${r.freq.toFixed(3)} MHz` : "NONE"; })()}
          </span>
          <span className="sep-v" style={{ height: 16 }}/>
          <span className="cap">GLOBAL PTT</span>
          <KeyChip binding={app.globalPtt} onRebind={k => setApp({ globalPtt: k })} />
          <div className="flex"/>
          <span className="cap">TRAFFIC</span>
          <VU level={0.4} segs={12} />
        </div>

        {/* Radios area */}
        <div style={{ flex: 1, overflow: "auto", padding: 16, minHeight: 0 }}>
          {radios.length === 0 ? (
            <StateCard
              glyph="radio"
              title="No radios configured"
              message="Add a radio strip to begin tuning. You can load a Radio Profile to restore a saved panel layout."
              action={<div className="row gap-3"><button className="btn btn-primary" onClick={addRadio}><Icon name="plus" size={11}/> ADD RADIO</button><button className="btn" onClick={() => setApp({ view: "profiles" })}>LOAD PROFILE</button></div>}
            />
          ) : compact ? (
            <div className="col gap-4">
              {radios.map(r => <RadioStrip key={r.id} radio={r} onChange={n => update(r.id, n)} />)}
            </div>
          ) : app.layoutPreset === "power" ? (
            <PowerLayout radios={radios} update={update} app={app} setApp={setApp} editLayout={editLayout} removeRadio={removeRadio} />
          ) : (
            <DefaultLayout radios={radios} update={update} app={app} setApp={setApp} editLayout={editLayout} removeRadio={removeRadio} />
          )}
        </div>
      </div>

      {/* Side drawer */}
      {drawerOpen && <CommsDrawer app={app} setApp={setApp} tab={drawerTab} setTab={setDrawerTab} />}
    </div>
  );
}

function DefaultLayout({ radios, update, app, setApp, editLayout, removeRadio }) {
  return (
    <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(420px, 1fr))", gap: 14 }}>
      {radios.map(r => (
        <div key={r.id} style={{ position: "relative" }}>
          <RadioWidget radio={r} onChange={n => update(r.id, n)} selected={app.selectedRadioId === r.id} onSelect={id => setApp({ selectedRadioId: id })} />
          {editLayout && (
            <button className="btn btn-danger btn-icon btn-sm" style={{ position: "absolute", top: 6, right: 38 }} onClick={() => removeRadio(r.id)}>
              <Icon name="x" size={10} />
            </button>
          )}
        </div>
      ))}
    </div>
  );
}

function PowerLayout({ radios, update, app, setApp, editLayout, removeRadio }) {
  if (radios.length === 0) return null;
  const [primary, ...rest] = radios;
  return (
    <div style={{ display: "grid", gridTemplateColumns: "minmax(440px, 1.4fr) 1fr", gap: 14, alignItems: "start" }}>
      <RadioWidget radio={primary} onChange={n => update(primary.id, n)} size="lg" selected={app.selectedRadioId === primary.id} onSelect={id => setApp({ selectedRadioId: id })} />
      <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 10 }}>
        {rest.slice(0, 6).map(r => (
          <RadioWidget key={r.id} radio={r} onChange={n => update(r.id, n)} size="sm" selected={app.selectedRadioId === r.id} onSelect={id => setApp({ selectedRadioId: id })} />
        ))}
      </div>
      {rest.length > 6 && (
        <div style={{ gridColumn: "1 / -1", display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(280px, 1fr))", gap: 10 }}>
          {rest.slice(6).map(r => (
            <RadioWidget key={r.id} radio={r} onChange={n => update(r.id, n)} size="sm" selected={app.selectedRadioId === r.id} onSelect={id => setApp({ selectedRadioId: id })} />
          ))}
        </div>
      )}
    </div>
  );
}

function CommsDrawer({ app, setApp, tab, setTab }) {
  return (
    <div style={{ background: "var(--bg-0)", borderLeft: "1px solid var(--bd-1)", display: "flex", flexDirection: "column", minHeight: 0 }}>
      <div className="tabs" style={{ flexShrink: 0 }}>
        <span className={`tab ${tab === "recent" ? "active" : ""}`} onClick={() => setTab("recent")}>Recent</span>
        <span className={`tab ${tab === "presets" ? "active" : ""}`} onClick={() => setTab("presets")}>Presets</span>
      </div>
      <div style={{ flex: 1, overflow: "auto", padding: 10 }}>
        {tab === "recent" ? <RecentTraffic app={app} /> : <Presets app={app} setApp={setApp} />}
      </div>
    </div>
  );
}

function RecentTraffic({ app }) {
  return (
    <div className="col gap-3">
      {app.recentTraffic.map((t, i) => (
        <div key={i} className="tac" style={{ padding: 0 }}>
          <div className="row acenter gap-3" style={{ padding: "6px 8px" }}>
            <span className="mono" style={{ fontSize: 9, color: "var(--tx-3)" }}>{t.time}</span>
            <span style={{ fontSize: 11, color: t.self ? "var(--ac-primary)" : "var(--tx-0)", fontWeight: 500 }}>{t.from}</span>
            <span className="flex"/>
            <span className="mono" style={{ fontSize: 9, color: "var(--tx-2)" }}>{t.freq}</span>
            <span className="mono" style={{ fontSize: 9, color: "var(--tx-4)" }}>{t.dur}s</span>
          </div>
        </div>
      ))}
    </div>
  );
}

function Presets({ app, setApp }) {
  return (
    <div className="col gap-3">
      <div className="row gap-3">
        <input className="input flex" placeholder="Search presets…"/>
        <button className="btn btn-icon"><Icon name="plus" size={12}/></button>
      </div>
      {app.presets.map((p, i) => (
        <div key={i} className="tac" draggable style={{ cursor: "grab" }}>
          <div className="tac-body" style={{ padding: 8 }}>
            <div className="row acenter gap-3">
              <Icon name="radio" size={12} style={{ color: p.color || "var(--ac-primary)" }}/>
              <span style={{ fontSize: 12, color: "var(--tx-0)" }}>{p.name}</span>
              <span className="flex"/>
              <span className="mono" style={{ fontSize: 10, color: "var(--ac-lcd)", background: "var(--bg-lcd)", padding: "2px 6px", borderRadius: 2 }}>{p.freq.toFixed(3)}</span>
            </div>
            <div className="row acenter gap-3" style={{ marginTop: 4 }}>
              <span className="cap-dim" style={{ fontSize: 9 }}>{p.tag}</span>
              {p.enc && <span className="cap" style={{ color: "var(--ac-warn)" }}><Icon name="lock" size={9}/> ENC</span>}
            </div>
          </div>
        </div>
      ))}
    </div>
  );
}

/* Compact overlay window */
function OverlayWindow({ app, setApp }) {
  const radios = app.radios.slice(0, 3);
  return (
    <div style={{
      position: "fixed", top: 20, right: 20, zIndex: 200,
      background: "rgba(3,7,13,0.92)",
      border: "1px solid var(--bd-3)",
      borderRadius: 6,
      padding: 8,
      boxShadow: "0 8px 40px rgba(0,0,0,0.8), 0 0 0 1px var(--ac-primary-glow)",
      backdropFilter: "blur(4px)",
    }}>
      <div className="row acenter between" style={{ marginBottom: 8 }}>
        <span className="cap" style={{ color: "var(--ac-primary)" }}><Icon name="broadcast" size={10}/> COMPACT OVERLAY</span>
        <div className="row acenter gap-2">
          <button className="btn btn-ghost btn-icon btn-sm"><Icon name="pin" size={10}/></button>
          <button className="btn btn-ghost btn-icon btn-sm" onClick={() => setApp({ overlayMode: false })}><Icon name="close" size={10}/></button>
        </div>
      </div>
      <div className="col gap-3">
        {radios.map(r => <RadioStrip key={r.id} radio={r} onChange={n => setApp({ radios: app.radios.map(x => x.id === r.id ? { ...x, ...n } : x) })} />)}
      </div>
    </div>
  );
}

function makeRadio(idx, opts = {}) {
  return {
    id: "r-" + Math.random().toString(36).slice(2, 8),
    index: idx,
    name: `Radio ${idx}`,
    freq: 118.000 + idx * 2,
    enc: false,
    encChan: 1,
    channel: 1,
    volume: 70,
    balance: 0,
    muted: false,
    pttKey: `F${idx}`,
    selectKey: `Ctrl+${idx}`,
    rxActive: false,
    lastTalker: "",
    tuned: 0,
    unread: 0,
    role: false,
    ...opts,
  };
}

Object.assign(window, { ScreenComms, OverlayWindow, makeRadio });
