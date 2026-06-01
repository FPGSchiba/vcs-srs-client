/* global React, Icon, Toggle, Panel, Field, Tac, StateCard, Segmented, KeyChip, HealthBar, StatusPill, VU, LcdFreq, Knob */
/* ============================================================
   Profile Manager · Messages · Transmission Log · Notifications · Style System
   ============================================================ */
const { useState: useMiscState } = React;

/* ----------------- Profile Manager ----------------- */
function ScreenProfiles({ app, setApp }) {
  const [active, setActive] = useMiscState(app.profile.id);
  return (
    <div style={{ height: "100%", display: "grid", gridTemplateColumns: "1fr 360px", minHeight: 0 }}>
      <div style={{ overflow: "auto", padding: 16, minHeight: 0 }}>
        <Panel title="◆ PROFILES DIRECTORY" accessory={
          <div className="row gap-3">
            <input className="input mono" defaultValue="C:\Users\Schiba\Documents\Vanguard\Profiles" style={{ width: 380 }}/>
            <button className="btn btn-sm"><Icon name="folder" size={11}/> BROWSE</button>
            <button className="btn btn-sm"><Icon name="folder" size={11}/> OPEN</button>
          </div>
        }>
          <div className="row acenter gap-4" style={{ marginBottom: 12 }}>
            <input className="input flex" placeholder="Filter profiles by name…"/>
            <button className="btn"><Icon name="upload" size={11}/> IMPORT FROM FILE</button>
            <button className="btn btn-primary"><Icon name="save" size={11}/> SAVE CURRENT AS NEW</button>
          </div>
          <table className="tbl">
            <thead><tr><th style={{ width: 60 }}>Preview</th><th>Name</th><th>Filename</th><th>Modified</th><th>Radios</th><th>Layout</th><th style={{ width: 220 }}>Actions</th></tr></thead>
            <tbody>
              {app.profiles.map(p => (
                <tr key={p.id} style={{ background: active === p.id ? "rgba(96,165,250,0.04)" : undefined }} onClick={() => setActive(p.id)}>
                  <td><LayoutPreview kind={p.layout} count={p.radioCount}/></td>
                  <td>
                    <span style={{ color: "var(--tx-0)", fontWeight: 500 }}>{p.name}</span>
                    {p.id === app.profile.id && <span className="cap" style={{ color: "var(--ac-primary)", marginLeft: 8 }}>● ACTIVE</span>}
                  </td>
                  <td className="mono" style={{ fontSize: 10, color: "var(--tx-3)" }}>{p.file}</td>
                  <td className="mono" style={{ fontSize: 10 }}>{p.modified}</td>
                  <td className="mono">{p.radioCount}</td>
                  <td className="cap-dim">{p.layout}</td>
                  <td>
                    <div className="row gap-2">
                      <button className="btn btn-sm" onClick={() => setApp({ profile: { ...p, dirty: false }, view: "comms" })}>LOAD</button>
                      <button className="btn btn-sm"><Icon name="copy" size={10}/></button>
                      <button className="btn btn-sm"><Icon name="edit" size={10}/></button>
                      <button className="btn btn-sm"><Icon name="download" size={10}/></button>
                      <button className="btn btn-sm btn-danger"><Icon name="trash" size={10}/></button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </Panel>
      </div>

      <div style={{ borderLeft: "1px solid var(--bd-1)", background: "var(--bg-0)", padding: 16, overflow: "auto" }}>
        <span className="cap" style={{ color: "var(--ac-primary)" }}>PROFILE DETAILS</span>
        {(() => {
          const p = app.profiles.find(x => x.id === active);
          if (!p) return null;
          return (
            <div className="col gap-4" style={{ marginTop: 10 }}>
              <Field label="Name"><span style={{ fontSize: 14, color: "var(--tx-0)" }}>{p.name}</span></Field>
              <Field label="Filename"><span className="mono" style={{ color: "var(--tx-2)" }}>{p.file}</span></Field>
              <Field label="Last modified"><span className="mono">{p.modified}</span></Field>
              <Field label="Radios"><span className="mono" style={{ color: "var(--tx-0)" }}>{p.radioCount} configured</span></Field>
              <Field label="Layout"><span className="cap-dim">{p.layout}</span></Field>
              <Field label="Description"><span style={{ color: "var(--tx-2)", textWrap: "pretty" }}>{p.desc}</span></Field>
              <Field label="Author"><span style={{ color: "var(--tx-2)" }}>{p.author}</span></Field>
              <div className="cap" style={{ color: "var(--ac-primary)", marginTop: 8 }}>EXAMPLE CONTENT</div>
              <pre className="mono" style={{
                background: "var(--bg-1)", border: "1px solid var(--bd-1)", borderRadius: 4,
                padding: 10, fontSize: 10, color: "var(--tx-2)",
                overflow: "auto", margin: 0, lineHeight: 1.5,
              }}>
{`{
  "name": "${p.name}",
  "layout": "${p.layout}",
  "radios": [
    { "n": 1, "name": "Fleet", "freq": 118.500, "enc": false },
    { "n": 2, "name": "Wing",  "freq": 122.750, "enc": true,  "key": 4 },
    ...
  ],
  "overlay": { "anchor": "top-right", "strips": 2 }
}`}
              </pre>
            </div>
          );
        })()}
      </div>
    </div>
  );
}

function LayoutPreview({ kind, count }) {
  // Render a tiny SVG schematic of the layout
  if (kind === "2×2") return (
    <svg width="42" height="28" viewBox="0 0 42 28">
      {[0,1,2,3].map(i => (
        <rect key={i} x={1 + (i%2)*21} y={1 + Math.floor(i/2)*14} width="19" height="12" fill="none" stroke="var(--ac-primary)" opacity="0.6"/>
      ))}
    </svg>
  );
  if (kind === "POWER") return (
    <svg width="42" height="28" viewBox="0 0 42 28">
      <rect x="1" y="1" width="22" height="26" fill="none" stroke="var(--ac-primary)" opacity="0.8"/>
      <rect x="25" y="1" width="16" height="6" fill="none" stroke="var(--ac-primary)" opacity="0.6"/>
      <rect x="25" y="9" width="16" height="6" fill="none" stroke="var(--ac-primary)" opacity="0.6"/>
      <rect x="25" y="17" width="16" height="6" fill="none" stroke="var(--ac-primary)" opacity="0.6"/>
    </svg>
  );
  if (kind === "STRIP") return (
    <svg width="42" height="28" viewBox="0 0 42 28">
      <rect x="1" y="2" width="40" height="6" fill="none" stroke="var(--ac-primary)" opacity="0.6"/>
      <rect x="1" y="11" width="40" height="6" fill="none" stroke="var(--ac-primary)" opacity="0.6"/>
      <rect x="1" y="20" width="40" height="6" fill="none" stroke="var(--ac-primary)" opacity="0.6"/>
    </svg>
  );
  return null;
}

/* ----------------- Messages (text channels) ----------------- */
function ScreenMessages({ app, setApp }) {
  const [chanId, setChanId] = useMiscState(app.radios[0]?.id || null);
  const [text, setText] = useMiscState("");
  const chan = app.radios.find(r => r.id === chanId) || app.radios[0];
  return (
    <div style={{ height: "100%", display: "grid", gridTemplateColumns: "260px 1fr 240px", minHeight: 0 }}>
      <div style={{ borderRight: "1px solid var(--bd-1)", background: "var(--bg-0)", padding: 10, overflow: "auto" }}>
        <span className="cap" style={{ color: "var(--ac-primary)" }}>CHANNELS</span>
        <div className="col gap-2" style={{ marginTop: 10 }}>
          {app.radios.map(r => (
            <div key={r.id} onClick={() => setChanId(r.id)} style={{
              padding: 8, borderRadius: 4, cursor: "pointer",
              background: chanId === r.id ? "rgba(96,165,250,0.08)" : "var(--bg-2)",
              borderLeft: chanId === r.id ? "2px solid var(--ac-primary)" : "2px solid transparent",
              border: "1px solid var(--bd-2)",
            }}>
              <div className="row between acenter">
                <span style={{ fontSize: 12, color: "var(--tx-0)" }}>{r.name}</span>
                {r.unread > 0 && <span style={{ background: "var(--ac-alert)", color: "#fff", fontFamily: "var(--ff-mono)", fontSize: 9, padding: "0 5px", borderRadius: 999 }}>{r.unread}</span>}
              </div>
              <div className="mono" style={{ fontSize: 10, color: "var(--ac-lcd)", marginTop: 2 }}>{r.freq.toFixed(3)} · {r.enc ? "ENC" : "OPEN"}</div>
            </div>
          ))}
        </div>
      </div>
      {chan ? (
        <div style={{ display: "flex", flexDirection: "column", minHeight: 0 }}>
          <div style={{ padding: "10px 16px", borderBottom: "1px solid var(--bd-1)", background: "var(--bg-0)" }}>
            <div className="row acenter gap-4">
              <span style={{ fontSize: 13, color: "var(--tx-0)" }}>#{chan.name.toLowerCase().replace(/\s+/g,"-")}</span>
              <span className="mono" style={{ fontSize: 11, color: "var(--ac-lcd)", background: "var(--bg-lcd)", padding: "1px 8px", borderRadius: 2 }}>{chan.freq.toFixed(3)} MHz</span>
              <span className="cap-dim">MIRROR OF VOICE FREQUENCY</span>
              <span className="flex"/>
              <span className="cap-dim">7 LISTENING</span>
            </div>
          </div>
          <div style={{ flex: 1, overflow: "auto", padding: 16, minHeight: 0 }}>
            <div className="col gap-3">
              {MESSAGES.map((m, i) => (
                <div key={i} className="row" style={{ gap: 10 }}>
                  <div style={{
                    width: 28, height: 28, flex: "0 0 28px",
                    border: "1px solid var(--bd-2)", borderRadius: 4,
                    display: "grid", placeItems: "center",
                    color: m.color || "var(--ac-primary)", fontFamily: "var(--ff-mono)", fontSize: 10,
                  }}>{m.from.slice(0,2).toUpperCase()}</div>
                  <div className="col" style={{ flex: 1, minWidth: 0 }}>
                    <div className="row acenter gap-3">
                      <span style={{ fontSize: 12, color: m.self ? "var(--ac-primary)" : "var(--tx-0)" }}>{m.from}</span>
                      <span className="mono" style={{ fontSize: 9, color: "var(--tx-4)" }}>{m.time}</span>
                      {m.cmd && <span className="cap" style={{ color: "var(--ac-warn)" }}>/{m.cmd}</span>}
                    </div>
                    <div style={{ color: "var(--tx-1)", fontSize: 12, marginTop: 2, textWrap: "pretty" }}>{m.body}</div>
                  </div>
                </div>
              ))}
            </div>
          </div>
          <div style={{ padding: 12, borderTop: "1px solid var(--bd-1)", background: "var(--bg-0)" }}>
            {text.startsWith("/") && (
              <div className="tac" style={{ marginBottom: 6 }}>
                <div className="tac-body" style={{ padding: 6 }}>
                  <div className="cap-dim" style={{ marginBottom: 4 }}>BREVITY CODES</div>
                  <div className="col gap-2">
                    {["/ack","/wilco","/bingo","/winchester","/contact","/sitrep","/coords","/holding"].filter(c => c.startsWith(text)).map(c => (
                      <div key={c} className="row gap-3" style={{ fontFamily: "var(--ff-mono)", fontSize: 11 }}>
                        <span style={{ color: "var(--ac-primary)" }}>{c}</span>
                        <span style={{ color: "var(--tx-3)" }}>{BREVITY[c]}</span>
                      </div>
                    ))}
                  </div>
                </div>
              </div>
            )}
            <div className="row gap-3 acenter">
              <input className="input flex" placeholder={`Message #${chan.name.toLowerCase().replace(/\s+/g,"-")} — type / for brevity codes`} value={text} onChange={e => setText(e.target.value)}/>
              <button className="btn btn-primary"><Icon name="broadcast" size={11}/> SEND</button>
            </div>
          </div>
        </div>
      ) : <StateCard glyph="chat" title="No channel" message="Select a channel from the list."/>}
      <div style={{ borderLeft: "1px solid var(--bd-1)", background: "var(--bg-0)", padding: 16, overflow: "auto" }}>
        <span className="cap" style={{ color: "var(--ac-primary)" }}>TUNED ON THIS FREQ</span>
        <div className="col gap-2" style={{ marginTop: 10 }}>
          {["FPGSchiba","FPGElphi","Dabble","Spaceharvest","JohnMckeel","Vanderwolf","Mokushiroku"].map(n => (
            <div key={n} className="row acenter gap-3" style={{ padding: 6, border: "1px solid var(--bd-1)", borderRadius: 4 }}>
              <div style={{ width: 6, height: 6, borderRadius: "50%", background: "var(--ac-ok)" }}/>
              <span style={{ fontSize: 12 }}>{n}</span>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
const BREVITY = {
  "/ack": "Acknowledged",
  "/wilco": "Will comply",
  "/bingo": "Fuel critical",
  "/winchester": "Out of munitions",
  "/contact": "Enemy contact",
  "/sitrep": "Situation report",
  "/coords": "Coordinates",
  "/holding": "Holding position",
};
const MESSAGES = [
  { from: "FPGElphi", time: "21:14:01", body: "Engineers, confirm power triangle is set 30/70/0 prior to QT spool." },
  { from: "Vanderwolf", time: "21:14:21", body: "/ack 30/70/0 confirmed on Persephone.", cmd: "ack" },
  { from: "ColdSpoke", time: "21:14:28", body: "Reclaimer matches.", cmd: null },
  { from: "FPGSchiba", time: "21:15:02", body: "Pirate signal at MICRO-7 NORTH. /contact, range 4.2km. Stand by gunners.", cmd: "contact", color: "var(--ac-warn)" },
  { from: "Deathtype", time: "21:15:08", body: "Black Lance pushing. /wilco.", cmd: "wilco" },
  { from: "I_Die_a_lot", time: "21:15:12", body: "Quickfade wing. Engaging.", cmd: null },
  { from: "FPGSchiba", time: "21:15:44", body: "Phoenix-Eye, get me eyes on dorsal aspect, ASAP.", self: true, color: "var(--ac-primary)" },
  { from: "Dabble", time: "21:15:51", body: "Wilco. Repositioning.", cmd: null },
];

/* ----------------- Transmission Log ----------------- */
function ScreenHistory({ app }) {
  const [filterChan, setFilterChan] = useMiscState("all");
  return (
    <div style={{ height: "100%", display: "flex", flexDirection: "column", minHeight: 0 }}>
      <div style={{ padding: "10px 16px", borderBottom: "1px solid var(--bd-1)", background: "var(--bg-0)", display: "flex", alignItems: "center", gap: 12 }}>
        <span className="cap">FILTER</span>
        <select className="input" value={filterChan} onChange={e => setFilterChan(e.target.value)} style={{ width: 220 }}>
          <option value="all">All channels</option>
          {app.radios.map(r => <option key={r.id} value={r.id}>R{String(r.index).padStart(2,"0")} · {r.name} · {r.freq.toFixed(3)}</option>)}
        </select>
        <span className="sep-v" style={{ height: 20 }}/>
        <span className="cap">TIME</span>
        <Segmented value="1h" onChange={() => {}} options={[{value:"5m",label:"5M"},{value:"30m",label:"30M"},{value:"1h",label:"1H"},{value:"4h",label:"4H"},{value:"all",label:"ALL"}]}/>
        <span className="sep-v" style={{ height: 20 }}/>
        <input className="input flex" placeholder="Search sender or callsign…"/>
        <button className="btn"><Icon name="download" size={11}/> EXPORT CSV</button>
      </div>
      <div style={{ flex: 1, overflow: "auto", minHeight: 0 }}>
        <table className="tbl">
          <thead><tr><th style={{width:90}}>Time</th><th>Sender</th><th>Channel</th><th>Frequency</th><th style={{width:80}}>Duration</th><th style={{width:60}}>Replay</th></tr></thead>
          <tbody>
            {HISTORY.map((h, i) => (
              <tr key={i}>
                <td className="mono" style={{ color: "var(--tx-2)" }}>{h.time}</td>
                <td><span style={{ color: h.self ? "var(--ac-primary)" : "var(--tx-0)" }}>{h.from}</span></td>
                <td>{h.chan}</td>
                <td className="mono" style={{ color: "var(--ac-lcd)" }}>{h.freq}</td>
                <td className="mono">{h.dur}s</td>
                <td>
                  {h.rec ? <button className="btn btn-sm btn-icon"><Icon name="volume" size={11}/></button> : <span style={{ color: "var(--tx-4)" }}>—</span>}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
const HISTORY = [
  { time: "21:15:51", from: "Dabble", chan: "Fleet Common", freq: "118.500", dur: 3.2, rec: true },
  { time: "21:15:44", from: "FPGSchiba", chan: "Fleet Common", freq: "118.500", dur: 4.8, rec: true, self: true },
  { time: "21:15:12", from: "I_Die_a_lot", chan: "Gunners Net", freq: "122.750", dur: 2.1, rec: false },
  { time: "21:15:08", from: "Deathtype", chan: "Gunners Net", freq: "122.750", dur: 2.4, rec: false },
  { time: "21:15:02", from: "FPGSchiba", chan: "Fleet Common", freq: "118.500", dur: 8.1, rec: true, self: true },
  { time: "21:14:28", from: "ColdSpoke", chan: "Engineers Net", freq: "144.000", dur: 1.6, rec: true },
  { time: "21:14:21", from: "Vanderwolf", chan: "Engineers Net", freq: "144.000", dur: 2.9, rec: true },
  { time: "21:14:01", from: "FPGElphi", chan: "Engineers Net", freq: "144.000", dur: 7.4, rec: true },
  { time: "21:13:48", from: "Mokushiroku", chan: "Medics Net", freq: "139.500", dur: 5.0, rec: false },
  { time: "21:13:12", from: "JohnMckeel", chan: "Fleet Common", freq: "118.500", dur: 11.2, rec: true },
];

/* ----------------- Notifications panel ----------------- */
function ScreenNotifications({ app, setApp, openPopout, togglePopout }) {
  const [filter, setFilter] = useMiscState("all");
  const [expanded, setExpanded] = useMiscState(NOTIFS[0]?.id);
  const [showEmpty, setShowEmpty] = useMiscState(false);

  const filtered = showEmpty ? [] : NOTIFS.filter(n => filter === "all" || n.category === filter);

  return (
    <div style={{ height: "100%", display: "flex", flexDirection: "column", minHeight: 0 }}>
      {/* Header */}
      <div style={{ padding: "10px 14px", background: "var(--bg-0)", borderBottom: "1px solid var(--bd-1)", display: "flex", alignItems: "center", gap: 10 }}>
        <Icon name="bell" size={14} style={{ color: "var(--ac-primary)" }}/>
        <span style={{ fontSize: 13, color: "var(--tx-0)", fontWeight: 500 }}>Notifications</span>
        <span className="cap-dim mono">{filtered.length} OF {NOTIFS.length}</span>
        <span className="flex"/>
        <select className="input" value={filter} onChange={e => setFilter(e.target.value)} style={{ width: 160, height: 24 }}>
          <option value="all">All categories</option>
          {CATEGORIES.map(c => <option key={c.key} value={c.key}>{c.label}</option>)}
        </select>
        <button className="btn btn-sm" onClick={() => setApp({ unreadNotifications: 0 })}>MARK ALL READ</button>
        <button className="btn btn-sm btn-danger" onClick={() => setShowEmpty(true)}>CLEAR ALL</button>
      </div>

      {/* Body */}
      <div style={{ flex: 1, overflow: "auto", minHeight: 0, padding: 10 }}>
        {filtered.length === 0 ? (
          <div className="state-card" style={{ marginTop: 32 }}>
            <div className="state-glyph"><Icon name="bell" size={20}/></div>
            <div className="state-title">ALL CLEAR</div>
            <div style={{ fontSize: 12, color: "var(--tx-3)", maxWidth: 320, textWrap: "pretty" }}>
              {showEmpty ? "You cleared every alert. New events will appear here as they happen." : "No notifications match the current filter."}
            </div>
            {showEmpty && <button className="btn btn-sm" style={{ marginTop: 8 }} onClick={() => setShowEmpty(false)}>RESTORE EXAMPLE FEED</button>}
          </div>
        ) : (
          <div className="col gap-2">
            {filtered.map(n => (
              <NotifRow
                key={n.id}
                n={n}
                expanded={expanded === n.id}
                onToggle={() => setExpanded(x => x === n.id ? null : n.id)}
                openPopout={openPopout}
                togglePopout={togglePopout}
                setApp={setApp}
              />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

function NotifRow({ n, expanded, onToggle, openPopout, togglePopout, setApp }) {
  const cat = CATEGORIES.find(c => c.key === n.category) || CATEGORIES[0];
  return (
    <div style={{
      border: "1px solid var(--bd-2)",
      borderLeft: `2px solid ${cat.color}`,
      borderRadius: 3,
      background: n.unread ? "var(--bg-2)" : "var(--bg-1)",
      overflow: "hidden",
    }}>
      <div onClick={onToggle} style={{ padding: "10px 12px", cursor: "pointer", display: "grid", gridTemplateColumns: "auto 1fr auto auto", gap: 10, alignItems: "center" }}>
        <div style={{ display: "grid", placeItems: "center", width: 24, height: 24, color: cat.color }}>
          <Icon name={n.icon} size={14}/>
        </div>
        <div className="col" style={{ minWidth: 0 }}>
          <div className="row acenter gap-3">
            <span className="cap" style={{ color: cat.color }}>{cat.label.toUpperCase()}</span>
            {n.unread && <span style={{ width: 6, height: 6, borderRadius: "50%", background: "var(--ac-primary)", boxShadow: "0 0 4px var(--ac-primary)" }}/>}
          </div>
          <div style={{ fontSize: 12, color: "var(--tx-0)", marginTop: 2, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: expanded ? "normal" : "nowrap" }}>{n.title}</div>
        </div>
        <span className="mono" style={{ fontSize: 10, color: "var(--tx-3)" }}>{n.time}</span>
        <Icon name={expanded ? "chevronU" : "chevronD"} size={11} style={{ color: "var(--tx-3)" }}/>
      </div>
      {expanded && (
        <div style={{ padding: "0 12px 12px 46px", color: "var(--tx-2)", fontSize: 12, lineHeight: 1.6 }}>
          <div style={{ textWrap: "pretty" }}>{n.body}</div>
          {n.context && (
            <div className="mono" style={{ marginTop: 8, padding: 8, background: "var(--bg-1)", border: "1px solid var(--bd-1)", borderRadius: 3, fontSize: 11, color: "var(--tx-2)" }}>
              {Object.entries(n.context).map(([k, v]) => (
                <div key={k} className="row gap-3" style={{ padding: "1px 0" }}>
                  <span style={{ color: "var(--tx-3)", width: 80 }}>{k.toUpperCase()}</span>
                  <span style={{ color: "var(--tx-0)" }}>{v}</span>
                </div>
              ))}
            </div>
          )}
          {n.actions && (
            <div className="row gap-2" style={{ marginTop: 10 }}>
              {n.actions.map(a => (
                <button key={a.label}
                  className={`btn btn-sm ${a.primary ? "btn-primary" : ""}`}
                  onClick={() => {
                    if (a.action === "open-comms" && togglePopout) togglePopout("comms");
                    if (a.action === "open-ship" && togglePopout) togglePopout("ship");
                    if (a.action === "view-op" && setApp) setApp({ view: "operationDetail", operationId: "op-starwalk" });
                  }}>
                  <Icon name={a.icon || "chevron"} size={11}/> {a.label}
                </button>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

const CATEGORIES = [
  { key: "distress", label: "Distress",  color: "#ef4f4f" },
  { key: "fleet",    label: "Fleet alert", color: "#f5a524" },
  { key: "sync",     label: "Sync",      color: "#f5a524" },
  { key: "comms",    label: "Comms",     color: "#60a5fa" },
  { key: "profile",  label: "Profile",   color: "#4ade80" },
  { key: "system",   label: "System",    color: "#a78bfa" },
  { key: "operation", label: "Operation", color: "#60a5fa" },
];

const NOTIFS = [
  {
    id: 1, category: "distress", icon: "sos", unread: true,
    title: "DISTRESS BEACON · Tide-Walker", time: "21:15:02",
    body: "Spaceharvest broadcast a fleet-wide distress on 118.500. Tide-Walker reports engines at 8%, port shield collapsed, QT drive offline. Last known position MICRO-7 NORTH.",
    context: { from: "Spaceharvest", ship: "Tide-Walker", freq: "118.500 MHz", location: "STANTON · MICRO-7 N" },
    actions: [
      { label: "TUNE TO ORIGIN", icon: "broadcast", primary: true, action: "open-comms" },
      { label: "OPEN OPERATION", icon: "fleet", action: "view-op" },
    ],
  },
  {
    id: 2, category: "sync", icon: "sync", unread: true,
    title: "Ship Mode sync error · Port Engine", time: "21:14:50",
    body: "Component 'Port Engine' failed to broadcast 3 consecutive updates. Local edits are preserved; other crew may see stale state until sync resumes.",
    context: { component: "Port Engine", retries: "3", "last sync": "21:14:11" },
    actions: [
      { label: "RETRY SYNC", icon: "refresh", primary: true },
      { label: "OPEN SHIP MODE", icon: "ship", action: "open-ship" },
    ],
  },
  {
    id: 3, category: "comms", icon: "users", unread: true,
    title: "Spaceharvest joined 118.500 (Fleet Common)", time: "21:13:14",
    body: "Spaceharvest tuned into Fleet Common after firing the distress beacon.",
    actions: [{ label: "TUNE TO 118.500", icon: "broadcast", action: "open-comms" }],
  },
  {
    id: 4, category: "profile", icon: "layout", unread: false,
    title: "Profile loaded · Fleet Op — Stanton", time: "21:12:01",
    body: "Loaded 6 radios across 3 wings. Overlay restored to top-right. PTT keybinds restored.",
  },
  {
    id: 5, category: "operation", icon: "fleet", unread: true,
    title: "You are now Engineer-of-Record on ARK-04", time: "21:11:22",
    body: "Engineer-of-Record changes propagate edit access in Ship Mode. Other engineers see your changes in real time.",
    actions: [{ label: "OPEN SHIP MODE", icon: "ship", action: "open-ship", primary: true }],
  },
  {
    id: 6, category: "fleet", icon: "broadcast", unread: false,
    title: "FC broadcast · Engage at will", time: "21:08:00",
    body: "FPGSchiba issued an Engage broadcast to all wings.",
  },
  {
    id: 7, category: "system", icon: "sensor", unread: false,
    title: "High latency · 124ms spike on voice-eu-02", time: "21:10:00",
    body: "Connection briefly spiked to 124ms. Returned to nominal within 2 seconds. No packet loss.",
  },
  {
    id: 8, category: "operation", icon: "ship", unread: false,
    title: "Op briefing posted · OP TIDEPOOL", time: "20:55:00",
    body: "FC FPGElphi posted the briefing for OP TIDEPOOL (Sat 22:00 UTC). 4 of 8 seats currently open.",
    actions: [{ label: "VIEW OPERATION", icon: "fleet", action: "view-op", primary: true }],
  },
];

/* ----------------- Style System (design system page) ----------------- */
function ScreenSystem({ app, setApp }) {
  return (
    <div style={{ overflow: "auto", padding: 16, minHeight: 0, height: "100%" }}>
      <div className="col gap-5">
        <Panel title="◆ COLOR SYSTEM" accessory={<span className="cap-dim">tactical · disciplined · premium</span>}>
          <div className="col gap-4">
            <SwatchRow label="SURFACES" items={[
              ["bg-0","#03070d"],["bg-1","#060d16"],["bg-2","#0a141f"],["bg-3","#0e1a28"],["bg-lcd","#060e0a"],
            ]}/>
            <SwatchRow label="BORDERS" items={[
              ["bd-1","#15273a"],["bd-2","#1d3147"],["bd-3","#2a4663"],
            ]}/>
            <SwatchRow label="TEXT" items={[
              ["tx-0","#eaf2fb"],["tx-1","#c7d4e2"],["tx-2","#8ba0b6"],["tx-3","#5b748b"],["tx-4","#3b556e"],
            ]}/>
            <SwatchRow label="ACCENTS" items={[
              ["primary · ice blue","#60a5fa"],["warn · amber","#f5a524"],["alert · red","#ef4f4f"],["ok · green","#4ade80"],["lcd · signal","#7af0a4"],["lcd-amber","#f5b94b"],["violet","#a78bfa"],
            ]}/>
          </div>
        </Panel>

        <Panel title="◆ TYPE">
          <div className="row gap-8">
            <div className="col gap-3" style={{ flex: 1 }}>
              <span className="cap">DISPLAY · SPACE GROTESK</span>
              <div style={{ fontSize: 32, color: "var(--tx-0)", letterSpacing: "0.04em" }}>Tactical communications</div>
              <div style={{ fontSize: 20, color: "var(--tx-0)" }}>Subtitle 20 / 600</div>
              <div style={{ fontSize: 14, color: "var(--tx-0)" }}>Body 14 / 500 — the disciplined voice of the fleet.</div>
              <div style={{ fontSize: 13, color: "var(--tx-1)" }}>Default 13 / 400 — used throughout the interface.</div>
              <div style={{ fontSize: 12, color: "var(--tx-2)" }}>Caption 12 / 400 — secondary labels.</div>
            </div>
            <div className="col gap-3" style={{ flex: 1 }}>
              <span className="cap">MONO · JETBRAINS MONO</span>
              <div className="mono" style={{ fontSize: 14, color: "var(--tx-0)" }}>radios[0].freq = 118.500</div>
              <div className="mono" style={{ fontSize: 12, color: "var(--tx-1)" }}>FFID VG-0271-DSC</div>
              <span className="cap" style={{ marginTop: 12 }}>ALL-CAPS LABEL · MONO 10/0.18em</span>
              <div className="lcd-screen" style={{ display: "inline-block", padding: "8px 14px" }}>
                <span className="lcd" style={{ fontSize: 26, color: "var(--ac-lcd)" }}>118.500</span>
                <span className="lcd" style={{ fontSize: 12, color: "var(--ac-lcd)", opacity: 0.55, marginLeft: 6 }}>MHz</span>
              </div>
            </div>
          </div>
        </Panel>

        <Panel title="◆ SPACING & RADIUS">
          <div className="row gap-6 acenter">
            {[2,4,6,8,12,16,24,32].map(s => (
              <div key={s} className="col center gap-2">
                <div style={{ width: 32, height: s, background: "var(--ac-primary)" }}/>
                <span className="cap mono">{s}</span>
              </div>
            ))}
            <span className="sep-v" style={{ height: 30 }}/>
            <div className="row gap-4 acenter">
              {[2,4,6,999].map(r => (
                <div key={r} className="col center gap-2">
                  <div style={{ width: 30, height: 30, background: "var(--bg-3)", border: "1px solid var(--bd-2)", borderRadius: r }}/>
                  <span className="cap mono">{r === 999 ? "pill" : r}</span>
                </div>
              ))}
            </div>
          </div>
        </Panel>

        <Panel title="◆ BUTTONS · TOGGLES · INPUTS">
          <div className="row gap-3" style={{ flexWrap: "wrap", marginBottom: 16 }}>
            <button className="btn btn-primary">PRIMARY</button>
            <button className="btn">SECONDARY</button>
            <button className="btn btn-danger">DANGER</button>
            <button className="btn btn-ghost">GHOST</button>
            <button className="btn btn-icon"><Icon name="settings" size={12}/></button>
            <button className="btn btn-primary btn-lg">PRIMARY LARGE</button>
            <button className="btn btn-sm">SMALL</button>
            <button className="btn" disabled>DISABLED</button>
          </div>
          <div className="row gap-6 acenter" style={{ marginBottom: 16 }}>
            <span className="cap">TOGGLE</span>
            <Toggle on={true} onChange={() => {}}/>
            <Toggle on={false} onChange={() => {}}/>
            <Toggle on={true} onChange={() => {}} lg/>
            <Toggle on={false} onChange={() => {}} lg/>
            <span className="sep-v" style={{ height: 24 }}/>
            <span className="cap">SEGMENTED</span>
            <Segmented value="a" onChange={() => {}} options={[{value:"a",label:"OPT A"},{value:"b",label:"OPT B"},{value:"c",label:"OPT C"}]}/>
          </div>
          <div className="row gap-3" style={{ flexWrap: "wrap" }}>
            <input className="input" placeholder="Standard input" style={{ width: 200 }}/>
            <input className="input" type="password" defaultValue="●●●●●●" style={{ width: 140 }}/>
            <select className="input" style={{ width: 160 }}><option>Dropdown</option></select>
            <input className="input mono" defaultValue="srs.vanguard.org:5002" style={{ width: 240 }}/>
            <div className="row acenter gap-3"><input className="input mono" defaultValue="C:\Profiles" style={{ width: 200 }}/><button className="btn"><Icon name="folder" size={11}/></button></div>
          </div>
        </Panel>

        <Panel title="◆ KNOBS · LCD · METERS">
          <div className="row gap-8 acenter">
            <div className="col center gap-3">
              <Knob value={70} label="VOL" onChange={() => {}}/>
              <span className="cap-dim">Knob, default</span>
            </div>
            <div className="col center gap-3">
              <Knob value={0} min={-50} max={50} center label="BAL L/R" onChange={() => {}} size={36}/>
              <span className="cap-dim">Knob, centered</span>
            </div>
            <div className="col center gap-3">
              <Knob value={100} label="GAIN" onChange={() => {}} size={56} ticks={11}/>
              <span className="cap-dim">Knob, large</span>
            </div>
            <span className="sep-v" style={{ height: 80 }}/>
            <div className="col gap-3">
              <span className="cap">LCD · GREEN</span>
              <LcdFreq freq={118.500} onChange={() => {}} size={28}/>
              <span className="cap">LCD · AMBER</span>
              <LcdFreq freq={122.750} amber onChange={() => {}} size={20}/>
            </div>
            <span className="sep-v" style={{ height: 80 }}/>
            <div className="col gap-3" style={{ width: 240 }}>
              <span className="cap">VU · 0%</span><VU level={0}/>
              <span className="cap">VU · 50%</span><VU level={0.5}/>
              <span className="cap">VU · 90% peak</span><VU level={0.95}/>
            </div>
          </div>
        </Panel>

        <Panel title="◆ STATUS PILLS · HEALTH BARS">
          <div className="row gap-3" style={{ flexWrap: "wrap", marginBottom: 16 }}>
            <StatusPill kind="available"/>
            <StatusPill kind="combat"/>
            <StatusPill kind="discipline"/>
            <StatusPill kind="afk"/>
            <span className="sep-v" style={{ height: 18 }}/>
            <StatusPill kind="nominal"/>
            <StatusPill kind="degraded"/>
            <StatusPill kind="critical"/>
            <StatusPill kind="offline"/>
            <StatusPill kind="disabled"/>
          </div>
          <div className="col gap-3" style={{ maxWidth: 360 }}>
            <div className="row acenter gap-3"><span className="cap" style={{ width: 80 }}>NOMINAL</span><HealthBar value={92}/><span className="mono" style={{ fontSize: 10, width: 30 }}>92%</span></div>
            <div className="row acenter gap-3"><span className="cap" style={{ width: 80 }}>DEGRADED</span><HealthBar value={52}/><span className="mono" style={{ fontSize: 10, width: 30 }}>52%</span></div>
            <div className="row acenter gap-3"><span className="cap" style={{ width: 80 }}>CRITICAL</span><HealthBar value={18}/><span className="mono" style={{ fontSize: 10, width: 30 }}>18%</span></div>
            <div className="row acenter gap-3"><span className="cap" style={{ width: 80 }}>OFFLINE</span><HealthBar value={0} state="offline"/><span className="mono" style={{ fontSize: 10, width: 30 }}>0%</span></div>
          </div>
        </Panel>

        <Panel title="◆ KEYBIND CHIPS">
          <div className="row gap-6 acenter">
            <KeyChip binding="Space" onRebind={() => {}}/>
            <KeyChip binding="F1" onRebind={() => {}}/>
            <KeyChip binding="Ctrl+1" onRebind={() => {}}/>
            <KeyChip binding="Alt+Shift+E" onRebind={() => {}}/>
            <KeyChip binding={null} onRebind={() => {}}/>
          </div>
        </Panel>

        <Panel title="◆ CONTAINERS · CARDS · TABS">
          <div className="row gap-5" style={{ alignItems: "stretch" }}>
            <Tac title="TACTICAL CONTAINER" style={{ flex: 1 }}>
              Body content with corner brackets and a tinted header strip.
            </Tac>
            <Panel title="STANDARD PANEL" style={{ flex: 1 }}>
              A standard panel uses a glow gradient on the header strip.
            </Panel>
            <div style={{ flex: 1 }}>
              <div className="tabs">
                <span className="tab active">TAB ONE</span>
                <span className="tab">TAB TWO</span>
                <span className="tab">TAB THREE</span>
              </div>
              <div style={{ padding: 12, color: "var(--tx-2)" }}>Tab content example.</div>
            </div>
          </div>
        </Panel>

        <Panel title="◆ EMPTY · ERROR · LOADING STATES">
          <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(260px, 1fr))", gap: 12 }}>
            <StateCard glyph="radio" title="No radios configured" message="Add a radio to begin tuning."/>
            <StateCard glyph="server" title="Server disconnected" message="Lost link to vanguard-prime. Reconnecting…" kind="warn"/>
            <StateCard glyph="mic" title="Microphone not found" message="No input device detected. Plug in a mic or pick one in Audio." kind="alert"/>
            <StateCard glyph="folder" title="No profile loaded" message="Open the Profile Manager to load a saved layout."/>
            <StateCard glyph="sync" title="Sync error" message="Component updates not broadcasting to crew. Retry?" kind="alert" action={<button className="btn btn-sm">RETRY SYNC</button>}/>
            <StateCard glyph="fleet" title="No active mission" message="Wait for the Fleet Commander to publish a mission template."/>
          </div>
        </Panel>

        <Panel title="◆ MOTION (described)">
          <div className="col gap-3 mono" style={{ fontSize: 12, color: "var(--tx-2)", lineHeight: 1.6 }}>
            <div>· Default transition · <span style={{ color: "var(--tx-0)" }}>120ms ease</span></div>
            <div>· Knob spin · <span style={{ color: "var(--tx-0)" }}>real-time pointer follow, no easing</span></div>
            <div>· LCD digit flicker · <span style={{ color: "var(--tx-0)" }}>120ms steps(2), brightness rebound</span></div>
            <div>· PTT pulse (when keyed) · <span style={{ color: "var(--tx-0)" }}>900ms ease-in-out, infinite</span></div>
            <div>· Toast in · <span style={{ color: "var(--tx-0)" }}>200ms ease, translateX(20px → 0)</span></div>
            <div>· Connection radar pulse · <span style={{ color: "var(--tx-0)" }}>1.2s linear, infinite</span></div>
          </div>
        </Panel>
      </div>
    </div>
  );
}

function SwatchRow({ label, items }) {
  return (
    <div className="row acenter gap-4">
      <span className="cap" style={{ width: 80, color: "var(--tx-3)" }}>{label}</span>
      <div className="row gap-3" style={{ flexWrap: "wrap" }}>
        {items.map(([name, hex]) => (
          <div key={name} className="col gap-2">
            <div style={{ width: 90, height: 36, background: hex, border: "1px solid var(--bd-2)", borderRadius: 2 }}/>
            <div className="mono" style={{ fontSize: 9, color: "var(--tx-3)" }}>{name}</div>
            <div className="mono" style={{ fontSize: 9, color: "var(--tx-4)" }}>{hex}</div>
          </div>
        ))}
      </div>
    </div>
  );
}

Object.assign(window, { ScreenProfiles, ScreenMessages, ScreenHistory, ScreenNotifications, ScreenSystem });
