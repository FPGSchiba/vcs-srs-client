/* global React, Icon, StatusPill, Toggle, Panel, Tac, Field, HealthBar, Segmented */
/* ============================================================
   Home page — main client landing
   ============================================================ */
const { useState: useHomeState } = React;

function ScreenHome({ app, setApp, togglePopout, popoutState }) {
  const tiles = [
    { key: "comms",         name: "Communications",  desc: "Tune, transmit, monitor",   icon: "comms",  meta: ["6 RADIOS","1 KEYED"], badge: 0 },
    { key: "fleet",         name: "Fleet Mode",      desc: "C2 dashboard · roster",     icon: "fleet",  meta: ["6 SHIPS","OP STARWALK"], badge: 0 },
    { key: "ship",          name: "Ship Mode",       desc: "Engineering & damage",      icon: "ship",   meta: ["16 COMPONENTS","2 DEGRADED","1 CRITICAL"], badge: 0 },
    { key: "messages",      name: "Messages",        desc: "Text channels per freq",    icon: "chat",   meta: ["6 CHANNELS"], badge: 3 },
    { key: "notifications", name: "Notifications",   desc: "Alerts, broadcasts, sync",  icon: "bell",   meta: ["7 CATEGORIES"], badge: app.unreadNotifications },
  ];

  // current / next op (joined by user) — pull from OPS_DATA
  const allOps = window.OPS_DATA || [];
  const current = allOps.find(o => o.participating && o.state === "Active");
  const next = allOps.find(o => o.participating && (o.state === "Briefing" || o.state === "Scheduled"));

  return (
    <div style={{ padding: 14, height: "100%", overflow: "hidden", display: "grid", gridTemplateRows: "auto auto 1fr auto", gap: 10 }}>
      {/* Identity strip */}
      <Panel title="◆ IDENTITY · STATUS" accessory={
        <div className="row gap-4 acenter">
          <span className="cap-dim mono">FFID {app.user.ffid}</span>
          <span className="cap" style={{ color: "var(--ac-primary)" }}>{app.user.org.toUpperCase()}</span>
        </div>
      }>
        <div className="row acenter gap-8">
          <div className="row acenter gap-4" style={{ minWidth: 240 }}>
            <div style={{
              width: 48, height: 48, borderRadius: 4,
              background: "linear-gradient(135deg, var(--bg-3), var(--bg-2))",
              border: "1px solid var(--bd-3)",
              display: "grid", placeItems: "center",
              fontFamily: "var(--ff-mono)", fontSize: 14, color: "var(--ac-primary)",
              fontWeight: 600,
            }}>
              {app.user.callsign.slice(0,2).toUpperCase()}
            </div>
            <div className="col" style={{ lineHeight: 1.2 }}>
              <span style={{ fontSize: 18, color: "var(--tx-0)" }}>{app.user.callsign}</span>
              <span className="cap-dim" style={{ marginTop: 2 }}>{app.user.org}</span>
            </div>
          </div>
          <span className="sep-v" style={{ height: 48 }}/>
          <div className="col gap-2">
            <span className="field-label">STATUS PRESENCE</span>
            <div className="row gap-2">
              {["available","combat","discipline","afk"].map(s => (
                <button
                  key={s}
                  className={`btn btn-sm ${app.user.status === s ? "btn-primary" : ""}`}
                  onClick={() => setApp({ user: { ...app.user, status: s } })}
                >
                  <StatusPill kind={s}/>
                </button>
              ))}
            </div>
          </div>
          <span className="sep-v" style={{ height: 48 }}/>
          <div className="col gap-3">
            <span className="field-label">CURRENT ASSIGNMENT</span>
            <div className="row acenter gap-3">
              <Icon name="ship" size={14} style={{ color: "var(--ac-primary)" }}/>
              <span style={{ color: "var(--tx-0)", fontSize: 13 }}>{app.assignment.ship}</span>
              <span style={{ color: "var(--tx-4)" }}>›</span>
              <span style={{ color: "var(--tx-1)" }}>{app.assignment.seat}</span>
              <span style={{ color: "var(--tx-4)" }}>›</span>
              <span style={{ color: "var(--ac-primary)" }}>{app.assignment.role}</span>
              <button className="btn btn-sm" style={{ marginLeft: 4 }} onClick={() => togglePopout("fleet")}>
                <Icon name="refresh" size={10}/> CHANGE
              </button>
            </div>
          </div>
        </div>
      </Panel>

      {/* Panel launcher tiles — 5 across */}
      <div>
        <div className="row between acenter" style={{ marginBottom: 6 }}>
          <span className="cap" style={{ color: "var(--ac-primary)" }}>PANELS</span>
          <span className="cap-dim">Pop-outs · resizable · click tile to open · click again to close</span>
        </div>
        <div style={{ display: "grid", gridTemplateColumns: "repeat(5, 1fr)", gap: 10 }}>
          {tiles.map(t => {
            const p = popoutState[t.key] || { open: false, docked: false, monitor: 1 };
            const state = !p.open ? "closed" : p.docked ? "docked" : p.monitor === 2 ? "monitor-2" : "floating";
            const stateLabel = state === "closed" ? "CLOSED" :
              state === "docked" ? "OPEN · DOCKED" :
              state === "monitor-2" ? "OPEN · MONITOR 2" :
              "OPEN · FLOATING";
            return (
              <div key={t.key}
                className={`launcher-tile ${p.open ? "is-open" : ""} ${state === "floating" ? "is-floating" : ""}`}
                onClick={() => togglePopout(t.key)}
              >
                {t.badge > 0 && (
                  <span style={{
                    position: "absolute", top: 10, right: 10,
                    background: "var(--ac-alert)", color: "#fff",
                    fontSize: 10, padding: "2px 6px", borderRadius: 999, fontFamily: "var(--ff-mono)",
                  }}>{t.badge}</span>
                )}
                <span className="lt-bg"><Icon name={t.icon} size={92} stroke={1}/></span>
                <div className="lt-head">
                  <Icon name={t.icon} size={18} style={{ color: "var(--ac-primary)" }}/>
                  <span className="lt-name">{t.name}</span>
                </div>
                <div className="lt-desc">{t.desc}</div>
                <div className="lt-meta">
                  {t.meta.map(m => <span key={m}><b>{m.split(" ")[0]}</b> {m.split(" ").slice(1).join(" ")}</span>)}
                </div>
                <span className="lt-state">
                  <span className="dot"/>
                  {stateLabel}
                  {p.open && <span style={{ marginLeft: 6, color: "var(--ac-primary-dim)" }}>· click to close</span>}
                </span>
              </div>
            );
          })}
        </div>
      </div>

      {/* Main content grid */}
      <div style={{ display: "grid", gridTemplateColumns: "1.4fr 1fr", gap: 12, minHeight: 0 }}>
        {/* Left: current / next op + recent activity */}
        <div className="col gap-3" style={{ minHeight: 0 }}>
          {/* Current op slim card */}
          {current && (
            <Panel title="◆ CURRENT OPERATION" accessory={
              <div className="row gap-3 acenter">
                <span className="cap" style={{ color: "var(--ac-ok)" }}>● LIVE · T+1H 24M</span>
                <button className="btn btn-sm" onClick={() => setApp({ view: "operationDetail", operationId: current.id })}>OPEN DETAIL</button>
                <button className="btn btn-sm" onClick={() => togglePopout("fleet")}><Icon name="fleet" size={11}/> FLEET MODE</button>
              </div>
            }>
              <div className="row acenter gap-6">
                <div className="col" style={{ flex: 1 }}>
                  <div className="row acenter gap-3">
                    <span style={{ fontSize: 18, color: "var(--tx-0)" }}>{current.name}</span>
                    <span className="pill" style={{ color: window.CAT_COLOR?.[current.category] || "var(--ac-primary)", borderColor: (window.CAT_COLOR?.[current.category] || "var(--ac-primary)") + "55" }}>
                      <span className="dot" style={{ background: window.CAT_COLOR?.[current.category] }}/>
                      {current.category.toUpperCase()}
                    </span>
                  </div>
                  <span className="cap-dim mono" style={{ marginTop: 4 }}>{current.start} · {current.duration} · FC {current.fc} · {current.filled}/{current.total} SEATS</span>
                </div>
                <div className="col" style={{ minWidth: 200 }}>
                  <span className="field-label">YOUR SLOT</span>
                  <span style={{ fontSize: 12, color: "var(--tx-0)" }}>{app.assignment.ship} · {app.assignment.seat} · {app.assignment.role}</span>
                </div>
              </div>
            </Panel>
          )}

          {/* Next op slim card */}
          {next && (
            <Panel title="◆ NEXT OPERATION" accessory={
              <button className="btn btn-sm" onClick={() => setApp({ view: "operationDetail", operationId: next.id })}>OPEN DETAIL</button>
            }>
              <div className="row acenter gap-6">
                <div className="col" style={{ flex: 1 }}>
                  <div className="row acenter gap-3">
                    <span style={{ fontSize: 14, color: "var(--tx-0)" }}>{next.name}</span>
                    <span className="pill" style={{ color: window.CAT_COLOR?.[next.category] || "var(--ac-primary)", borderColor: (window.CAT_COLOR?.[next.category] || "var(--ac-primary)") + "55" }}>
                      <span className="dot" style={{ background: window.CAT_COLOR?.[next.category] }}/>
                      {next.category.toUpperCase()}
                    </span>
                  </div>
                  <span className="cap-dim mono" style={{ marginTop: 2 }}>{next.start} · {next.duration} · FC {next.fc}</span>
                </div>
                <span className="cap" style={{ color: "var(--ac-warn)" }}>{next.state.toUpperCase()}</span>
              </div>
            </Panel>
          )}

          <Panel title="◆ RECENT ACTIVITY" accessory={
            <button className="btn btn-sm" onClick={() => setApp({ view: "operations" })}>
              <Icon name="fleet" size={11}/> ALL OPERATIONS
            </button>
          } style={{ flex: 1, display: "flex", flexDirection: "column", minHeight: 0 }}>
            <div style={{ overflow: "auto", flex: 1 }}>
              <div className="col gap-2">
                {RECENT_ACTIVITY.map((a,i) => (
                  <div key={i} className="row acenter gap-3" style={{ padding: 6, borderBottom: i < RECENT_ACTIVITY.length - 1 ? "1px solid var(--bd-1)" : "none" }}>
                    <Icon name={a.icon} size={12} style={{ color: a.color, flex: "0 0 12px" }}/>
                    <span className="mono" style={{ fontSize: 10, color: "var(--tx-3)", width: 50 }}>{a.time}</span>
                    <span style={{ fontSize: 11, color: "var(--tx-1)", textWrap: "pretty", flex: 1 }}>{a.text}</span>
                  </div>
                ))}
              </div>
            </div>
          </Panel>
        </div>

        {/* Right: topology + quick stats */}
        <div className="col gap-3">
          <Panel title="◆ TOPOLOGY">
            <div className="col gap-3">
              <div className="mini">
                <div className="mini-h"><span className="ttl">CONTROL SERVER</span><span className="cap" style={{ color: "var(--ac-ok)" }}>● LINKED</span></div>
                <div className="mini-v">vanguard-ctrl</div>
                <div className="mini-sub">EU-WEST · 18ms · v3.2.1</div>
              </div>
              <div className="mini">
                <div className="mini-h"><span className="ttl">VOICE SERVER</span><span className="cap" style={{ color: "var(--ac-ok)" }}>● LINKED</span></div>
                <div className="mini-v">voice-eu-02</div>
                <div className="mini-sub">EU-WEST · 28ms · LOAD 34%</div>
              </div>
              <div className="row gap-3">
                <div className="mini" style={{ flex: 1 }}>
                  <div className="mini-h"><span className="ttl">PLAYERS</span></div>
                  <div className="mini-v">18</div>
                  <div className="mini-sub">3 admins · 12 members · 3 guests</div>
                </div>
                <div className="mini" style={{ flex: 1 }}>
                  <div className="mini-h"><span className="ttl">PROFILE</span></div>
                  <div className="mini-v" style={{ fontSize: 14 }}>Fleet Op — Stanton</div>
                  <div className="mini-sub">6 radios · POWER</div>
                </div>
              </div>
              <button className="btn btn-sm" onClick={() => setApp({ view: "serverNetwork" })}>
                <Icon name="server" size={11}/> OPEN SERVER NETWORK
              </button>
            </div>
          </Panel>
        </div>
      </div>

      {/* Quick actions footer */}
      <div className="row gap-3 acenter" style={{ padding: "8px 12px", border: "1px solid var(--bd-1)", borderRadius: 4, background: "var(--bg-0)" }}>
        <span className="cap" style={{ color: "var(--ac-primary)" }}>QUICK ACTIONS</span>
        <button className="btn btn-sm" onClick={() => togglePopout("fleet")}><Icon name="ship" size={11}/> CHANGE SHIP / SEAT</button>
        <button className="btn btn-sm" onClick={() => setApp({ view: "profiles" })}><Icon name="layout" size={11}/> LOAD PROFILE</button>
        <button className="btn btn-sm"><Icon name="broadcast" size={11}/> START SOLO</button>
        <button className="btn btn-sm" onClick={() => setApp({ view: "operations" })}><Icon name="fleet" size={11}/> BROWSE OPERATIONS</button>
        <span className="flex"/>
        <span className="cap-dim mono">CTRL+H · home  ·  CTRL+1..5 · panels</span>
      </div>
    </div>
  );
}

const RECENT_ACTIVITY = [
  { icon: "broadcast", color: "var(--ac-alert)", time: "21:15", text: "Distress beacon · Spaceharvest · MICRO-7 NORTH" },
  { icon: "users",     color: "var(--ac-primary)", time: "21:14", text: "FPGElphi joined 144.000 (Engineers Net)" },
  { icon: "sync",      color: "var(--ac-warn)",  time: "21:14", text: "Ship Mode sync error · Port Engine (resolved)" },
  { icon: "layout",    color: "var(--ac-ok)",    time: "21:12", text: "Profile loaded · Fleet Op — Stanton" },
  { icon: "shield",    color: "var(--ac-primary)", time: "21:08", text: "Promoted to Engineer-of-Record on ARK-04" },
  { icon: "history",   color: "var(--tx-3)",     time: "yesterday", text: "Completed OP REDCROSS · 1h 30m" },
  { icon: "history",   color: "var(--tx-3)",     time: "yesterday", text: "Completed OP TIDEPOOL · 4h 12m" },
];

Object.assign(window, { ScreenHome });
