/* global React, Icon, HealthBar, StatusPill, Toggle, Tac, Panel, Field, StateCard */
/* ============================================================
   Ship Mode + Fleet Mode
   ============================================================ */
const { useState: useShipState } = React;

/* ----------------- Ship Mode ----------------- */
function ScreenShip({ app, setApp }) {
  const [editing, setEditing] = useShipState(null);
  const [adding, setAdding] = useShipState(false);
  const [showLog, setShowLog] = useShipState(true);
  const comp = app.shipComponents;

  const update = (id, patch) => {
    setApp({ shipComponents: comp.map(c => c.id === id ? { ...c, ...patch } : c) });
  };
  const remove = id => setApp({ shipComponents: comp.filter(c => c.id !== id) });

  return (
    <div style={{ height: "100%", display: "grid", gridTemplateColumns: "360px 1fr 320px", minHeight: 0 }}>
      {/* Left: component list */}
      <div style={{ borderRight: "1px solid var(--bd-1)", background: "var(--bg-0)", display: "flex", flexDirection: "column", minHeight: 0 }}>
        <div style={{ padding: 12, borderBottom: "1px solid var(--bd-1)" }}>
          <div className="row between acenter" style={{ marginBottom: 8 }}>
            <span className="cap">COMPONENTS</span>
            <button className="btn btn-primary btn-sm" onClick={() => setAdding(true)}><Icon name="plus" size={10}/> ADD</button>
          </div>
          <input className="input" placeholder="Filter…"/>
        </div>
        <div style={{ flex: 1, overflow: "auto" }}>
          {GROUPS.map(g => {
            const items = comp.filter(c => c.category === g.key);
            if (!items.length) return null;
            return (
              <div key={g.key}>
                <div style={{ padding: "10px 12px 4px", background: "var(--bg-1)", borderBottom: "1px solid var(--bd-1)", borderTop: "1px solid var(--bd-1)", display: "flex", alignItems: "center", gap: 8 }}>
                  <Icon name={g.icon} size={12} style={{ color: "var(--ac-primary)" }}/>
                  <span className="cap">{g.label}</span>
                  <span className="flex"/>
                  <span className="cap-dim mono">{items.length}</span>
                </div>
                {items.map(c => (
                  <div
                    key={c.id}
                    onClick={() => setEditing(c.id)}
                    style={{
                      padding: "8px 12px",
                      borderBottom: "1px solid var(--bd-1)",
                      cursor: "pointer",
                      background: editing === c.id ? "rgba(96,165,250,0.06)" : "transparent",
                      borderLeft: editing === c.id ? "2px solid var(--ac-primary)" : "2px solid transparent",
                    }}
                  >
                    <div className="row between acenter" style={{ marginBottom: 4 }}>
                      <span style={{ fontSize: 12, color: "var(--tx-0)" }}>{c.name}</span>
                      <StatusPill kind={c.state} />
                    </div>
                    <div className="row acenter gap-3">
                      <HealthBar value={c.health} state={c.state} />
                      <span className="mono" style={{ fontSize: 9, color: "var(--tx-3)", width: 28, textAlign: "right" }}>{c.health}%</span>
                    </div>
                  </div>
                ))}
              </div>
            );
          })}
        </div>
      </div>

      {/* Center: spatial widgets */}
      <div style={{ overflow: "auto", padding: 16, minHeight: 0 }}>
        {/* Ship identity + sync */}
        <Panel title={`◆ ${app.assignment.ship.toUpperCase()}`} accessory={
          <div className="row acenter gap-4">
            <span className="cap" style={{ color: "var(--tx-3)" }}>SYNC <span style={{ color: "var(--ac-ok)" }}>● LIVE</span></span>
            <span className="cap mono" style={{ color: "var(--tx-3)" }}>{app.shipSync.lastSync}</span>
            <div className="row acenter gap-3">
              <span className="cap">BROADCAST</span>
              <Toggle on={app.shipSync.broadcast} onChange={v => setApp({ shipSync: { ...app.shipSync, broadcast: v } })}/>
            </div>
          </div>
        }>
          <div className="row acenter gap-6">
            <Field label="Captain"><span style={{ fontSize: 13, color: "var(--tx-0)" }}>FPGSchiba</span></Field>
            <Field label="Crew"><span style={{ fontSize: 13, color: "var(--tx-0)" }}>4 of 5</span></Field>
            <Field label="Mission"><span style={{ fontSize: 13, color: "var(--ac-primary)" }}>OP STARWALK — Pirate Sweep</span></Field>
            <Field label="Mode"><span className="cap" style={{ color: "var(--ac-ok)" }}>READ/WRITE · ENGINEER</span></Field>
          </div>
        </Panel>

        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 12, marginTop: 12 }}>
          <Tac title="POWER TRIANGLE">
            <PowerTriangle weapons={70} shields={90} engines={50} />
          </Tac>
          <Tac title="SHIELD QUADRANTS">
            <ShieldRing quadrants={{ F: 94, R: 62, L: 78, B: 41, T: 88 }} />
          </Tac>
        </div>

        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr 1fr", gap: 12, marginTop: 12 }}>
          <ResourceGauge label="HYDROGEN FUEL" value={72} unit="%" color="var(--ac-primary)" />
          <ResourceGauge label="QUANTUM FUEL" value={48} unit="%" color="var(--ac-violet)" />
          <ResourceGauge label="CARGO" value={62} unit="SCU" max={144} color="var(--ac-warn)" />
        </div>

        {showLog && (
          <Panel title="DAMAGE LOG" style={{ marginTop: 12 }} accessory={
            <button className="btn btn-ghost btn-sm" onClick={() => setShowLog(false)}><Icon name="x" size={10}/></button>
          }>
            <div className="col gap-2" style={{ fontFamily: "var(--ff-mono)", fontSize: 11 }}>
              {DAMAGE_LOG.map((d, i) => (
                <div key={i} className="row acenter gap-4" style={{ padding: "4px 0", borderBottom: i < DAMAGE_LOG.length - 1 ? "1px solid var(--bd-1)" : "none" }}>
                  <span style={{ color: "var(--tx-3)", width: 60 }}>{d.time}</span>
                  <span style={{ color: d.state === "critical" ? "var(--ac-alert)" : d.state === "degraded" ? "var(--ac-warn)" : "var(--tx-1)", flex: 1 }}>{d.text}</span>
                  <StatusPill kind={d.state} mini/>
                </div>
              ))}
            </div>
          </Panel>
        )}
      </div>

      {/* Right: editor */}
      <div style={{ borderLeft: "1px solid var(--bd-1)", background: "var(--bg-0)", padding: 16, overflow: "auto" }}>
        {editing ? (
          <ComponentEditor
            component={comp.find(c => c.id === editing)}
            onChange={p => update(editing, p)}
            onDelete={() => { remove(editing); setEditing(null); }}
          />
        ) : (
          <StateCard glyph="ship" title="Select a Component" message="Click any component on the left to edit its health, state, and category. Changes broadcast to all crew on this ship."/>
        )}

        {/* Mission Objectives */}
        <Panel title="OP STARWALK · OBJECTIVES" style={{ marginTop: 16 }}>
          <div className="col gap-3" style={{ fontSize: 12 }}>
            <ObjRow done text="Rendezvous at QT marker MICRO-7"/>
            <ObjRow done text="Establish defensive perimeter"/>
            <ObjRow active text="Sweep ASTEROID FIELD-04 for pirate signals"/>
            <ObjRow text="Extract intel from disabled vessel"/>
            <ObjRow text="Return to base, debrief"/>
          </div>
        </Panel>
      </div>

      {adding && <ComponentAddModal onClose={() => setAdding(false)} onAdd={c => { setApp({ shipComponents: [...comp, { ...c, id: "c-" + Math.random().toString(36).slice(2,7) }] }); setAdding(false); }} />}
    </div>
  );
}

function ComponentEditor({ component, onChange, onDelete }) {
  if (!component) return null;
  const [synced, setSynced] = useShipState("synced");
  return (
    <div>
      <div className="row between acenter" style={{ marginBottom: 12 }}>
        <span className="cap" style={{ color: "var(--ac-primary)" }}>COMPONENT · EDIT</span>
        <span className="cap" style={{ color: synced === "syncing" ? "var(--ac-warn)" : "var(--ac-ok)" }}>{synced === "syncing" ? "● SYNCING" : "● SYNCED"}</span>
      </div>
      <div className="col gap-5">
        <Field label="Name">
          <input className="input" value={component.name} onChange={e => { onChange({ name: e.target.value }); setSynced("syncing"); setTimeout(() => setSynced("synced"), 400); }}/>
        </Field>
        <div className="row gap-4">
          <Field label="Category" style={{ flex: 1 }}>
            <select className="input" value={component.category} onChange={e => onChange({ category: e.target.value })}>
              {GROUPS.map(g => <option key={g.key} value={g.key}>{g.label}</option>)}
            </select>
          </Field>
          <Field label="Position" style={{ flex: 1 }}>
            <select className="input" value={component.position || ""} onChange={e => onChange({ position: e.target.value })}>
              <option value="">—</option>
              <option value="F">Forward</option>
              <option value="R">Right</option>
              <option value="L">Left</option>
              <option value="B">Back</option>
              <option value="T">Top</option>
            </select>
          </Field>
        </div>
        <Field label={`Health  ${component.health}%`}>
          <input type="range" min="0" max="100" value={component.health} onChange={e => { onChange({ health: +e.target.value }); setSynced("syncing"); setTimeout(() => setSynced("synced"), 400); }} style={{ width: "100%" }}/>
          <HealthBar value={component.health} state={component.state}/>
        </Field>
        <Field label="State">
          <div className="row gap-3" style={{ flexWrap: "wrap" }}>
            {["nominal","degraded","critical","offline","disabled"].map(s => (
              <button key={s} className={`btn btn-sm ${component.state === s ? "btn-primary" : ""}`} onClick={() => onChange({ state: s })}>
                {s.toUpperCase()}
              </button>
            ))}
          </div>
        </Field>
        <Field label="Notes">
          <textarea className="input" style={{ height: 70, padding: 8 }} value={component.notes || ""} onChange={e => onChange({ notes: e.target.value })} placeholder="Visible to all crew…"/>
        </Field>
        <div className="row gap-3" style={{ marginTop: 8 }}>
          <button className="btn btn-danger" onClick={onDelete}><Icon name="trash" size={11}/> REMOVE</button>
          <button className="btn flex">DUPLICATE</button>
        </div>
      </div>
    </div>
  );
}

function ComponentAddModal({ onClose, onAdd }) {
  const [c, setC] = useShipState({ name: "", category: "engines", health: 100, state: "nominal", position: "" });
  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal" onClick={e => e.stopPropagation()} style={{ width: 420 }}>
        <div className="panel-h">
          <span className="cap" style={{ color: "var(--ac-primary)" }}>ADD COMPONENT</span>
          <button className="btn btn-ghost btn-icon btn-sm" onClick={onClose}><Icon name="x" size={11}/></button>
        </div>
        <div className="col gap-4" style={{ padding: 16 }}>
          <Field label="Name"><input className="input" autoFocus value={c.name} onChange={e => setC({ ...c, name: e.target.value })} placeholder="e.g. Port Engine"/></Field>
          <Field label="Category">
            <select className="input" value={c.category} onChange={e => setC({ ...c, category: e.target.value })}>
              {GROUPS.map(g => <option key={g.key} value={g.key}>{g.label}</option>)}
            </select>
          </Field>
          <div className="row gap-3">
            <button className="btn flex" onClick={onClose}>CANCEL</button>
            <button className="btn btn-primary flex" disabled={!c.name} onClick={() => onAdd(c)}>ADD</button>
          </div>
        </div>
      </div>
    </div>
  );
}

function PowerTriangle({ weapons, shields, engines }) {
  // SVG triangle with three meters at corners
  return (
    <div style={{ position: "relative", height: 200, display: "grid", placeItems: "center" }}>
      <svg width="200" height="180" viewBox="0 0 200 180">
        <polygon points="100,20 180,160 20,160" fill="rgba(96,165,250,0.05)" stroke="var(--bd-2)" strokeWidth="1"/>
        <polygon points="100,40 165,150 35,150" fill="none" stroke="var(--ac-primary)" strokeWidth="1" opacity="0.4"/>
        {/* dot — position based on power distribution */}
        <circle
          cx={100 + (weapons - engines) * 0.4}
          cy={100 - (shields - 60) * 0.5}
          r="4" fill="var(--ac-primary)" style={{ filter: "drop-shadow(0 0 6px var(--ac-primary))" }}
        />
      </svg>
      {/* labels */}
      <div style={{ position: "absolute", top: 0, left: "50%", transform: "translateX(-50%)", textAlign: "center" }}>
        <Icon name="bolt" size={12} style={{ color: "var(--ac-primary)" }}/>
        <div className="cap" style={{ color: "var(--tx-2)" }}>WEAP</div>
        <div className="mono" style={{ fontSize: 14, color: "var(--ac-lcd)" }}>{weapons}</div>
      </div>
      <div style={{ position: "absolute", bottom: 10, left: 4, textAlign: "center" }}>
        <Icon name="shield" size={12} style={{ color: "var(--ac-primary)" }}/>
        <div className="cap" style={{ color: "var(--tx-2)" }}>SHLD</div>
        <div className="mono" style={{ fontSize: 14, color: "var(--ac-lcd)" }}>{shields}</div>
      </div>
      <div style={{ position: "absolute", bottom: 10, right: 4, textAlign: "center" }}>
        <Icon name="engine" size={12} style={{ color: "var(--ac-primary)" }}/>
        <div className="cap" style={{ color: "var(--tx-2)" }}>ENG</div>
        <div className="mono" style={{ fontSize: 14, color: "var(--ac-lcd)" }}>{engines}</div>
      </div>
    </div>
  );
}

function ShieldRing({ quadrants }) {
  const order = ["F","R","B","L"];
  return (
    <div style={{ position: "relative", height: 200, display: "grid", placeItems: "center" }}>
      <svg width="180" height="180" viewBox="0 0 180 180">
        <circle cx="90" cy="90" r="70" fill="none" stroke="var(--bd-2)"/>
        <circle cx="90" cy="90" r="55" fill="none" stroke="var(--bd-1)"/>
        {/* 4 arcs */}
        {order.map((dir, i) => {
          const v = quadrants[dir];
          const start = -45 + i * 90;
          const end = start + 90 - 4;
          const color = v > 70 ? "var(--ac-ok)" : v > 35 ? "var(--ac-warn)" : "var(--ac-alert)";
          const r = 70;
          const a1 = (start - 90) * Math.PI / 180;
          const a2 = (end - 90) * Math.PI / 180;
          const x1 = 90 + r * Math.cos(a1);
          const y1 = 90 + r * Math.sin(a1);
          const x2 = 90 + r * Math.cos(a2);
          const y2 = 90 + r * Math.sin(a2);
          return (
            <path
              key={dir}
              d={`M ${x1} ${y1} A ${r} ${r} 0 0 1 ${x2} ${y2}`}
              fill="none" stroke={color} strokeWidth="6" strokeLinecap="butt"
              opacity={v / 100 * 0.9 + 0.1}
              style={{ filter: `drop-shadow(0 0 4px ${color})` }}
            />
          );
        })}
        {/* T (top) center indicator */}
        <circle cx="90" cy="90" r="14" fill="none" stroke={quadrants.T > 70 ? "var(--ac-ok)" : quadrants.T > 35 ? "var(--ac-warn)" : "var(--ac-alert)"} strokeWidth="2"/>
        <text x="90" y="94" textAnchor="middle" fontSize="9" fill="var(--tx-0)" fontFamily="var(--ff-mono)">T·{quadrants.T}</text>
        <text x="90" y="22" textAnchor="middle" fontSize="9" fill="var(--tx-2)" fontFamily="var(--ff-mono)">F·{quadrants.F}</text>
        <text x="90" y="166" textAnchor="middle" fontSize="9" fill="var(--tx-2)" fontFamily="var(--ff-mono)">B·{quadrants.B}</text>
        <text x="14" y="93" textAnchor="middle" fontSize="9" fill="var(--tx-2)" fontFamily="var(--ff-mono)">L·{quadrants.L}</text>
        <text x="166" y="93" textAnchor="middle" fontSize="9" fill="var(--tx-2)" fontFamily="var(--ff-mono)">R·{quadrants.R}</text>
      </svg>
    </div>
  );
}

function ResourceGauge({ label, value, max = 100, unit = "%", color = "var(--ac-primary)" }) {
  const pct = (value / max) * 100;
  return (
    <Tac title={label}>
      <div className="col gap-3">
        <div className="mono" style={{ fontSize: 20, color: "var(--ac-lcd)", fontWeight: 600, textShadow: "0 0 6px var(--ac-lcd-dim)" }}>{value}<span style={{ fontSize: 12, color: "var(--tx-3)", marginLeft: 4 }}>/ {max}{unit}</span></div>
        <div style={{ height: 16, background: "var(--bg-1)", border: "1px solid var(--bd-1)", position: "relative", overflow: "hidden" }}>
          <div style={{ position: "absolute", inset: 0, background: "repeating-linear-gradient(90deg, transparent 0px, transparent 7px, var(--bd-1) 7px, var(--bd-1) 8px)" }}/>
          <div style={{ position: "absolute", left: 0, top: 0, bottom: 0, width: `${pct}%`, background: color, opacity: 0.6, boxShadow: `0 0 8px ${color}` }}/>
        </div>
      </div>
    </Tac>
  );
}

function ObjRow({ done, active, text }) {
  return (
    <div className="row acenter gap-3">
      <div style={{
        width: 12, height: 12, borderRadius: 2,
        border: `1px solid ${done ? "var(--ac-ok)" : active ? "var(--ac-primary)" : "var(--bd-2)"}`,
        background: done ? "var(--ac-ok)" : "transparent",
        display: "grid", placeItems: "center",
      }}>
        {done && <span style={{ color: "#000", fontSize: 8 }}>✓</span>}
        {active && <span style={{ width: 4, height: 4, background: "var(--ac-primary)", boxShadow: "0 0 4px var(--ac-primary)" }}/>}
      </div>
      <span style={{ color: done ? "var(--tx-3)" : active ? "var(--ac-primary)" : "var(--tx-1)", textDecoration: done ? "line-through" : "none" }}>{text}</span>
    </div>
  );
}

const GROUPS = [
  { key: "engines", label: "Engines", icon: "engine" },
  { key: "shields", label: "Shields", icon: "shield" },
  { key: "weapons", label: "Weapons", icon: "weapon" },
  { key: "power", label: "Power", icon: "bolt" },
  { key: "life-support", label: "Life Support", icon: "air" },
  { key: "sensors", label: "Sensors", icon: "sensor" },
  { key: "quantum", label: "Quantum Drive", icon: "quantum" },
  { key: "fuel", label: "Fuel", icon: "fuel" },
  { key: "cargo", label: "Cargo", icon: "cargo" },
];

const DAMAGE_LOG = [
  { time: "21:04:12", text: "Port engine: 67% → 41% (degraded)", state: "degraded" },
  { time: "21:03:58", text: "Aft shield: 78% → 41% (degraded)", state: "degraded" },
  { time: "21:03:30", text: "Starboard weapon S2: hit, 90% nominal", state: "nominal" },
  { time: "21:01:14", text: "QT drive: spool aborted", state: "critical" },
  { time: "20:58:01", text: "Life support · CO2 scrubber: nominal", state: "nominal" },
];

/* ----------------- Fleet Mode ----------------- */
function ScreenFleet({ app, setApp }) {
  const [shipPicker, setShipPicker] = useShipState(false);
  return (
    <div style={{ height: "100%", display: "grid", gridTemplateColumns: "1fr 360px", minHeight: 0 }}>
      <div style={{ overflow: "auto", padding: 16, minHeight: 0 }}>
        {/* Current assignment header */}
        <Panel title="◆ CURRENT ASSIGNMENT" accessory={
          <button className="btn btn-sm btn-primary" onClick={() => setShipPicker(true)}><Icon name="refresh" size={10}/> REASSIGN</button>
        }>
          <div className="row acenter gap-6">
            <Tag label="SHIP" value={app.assignment.ship} primary/>
            <span style={{ color: "var(--tx-4)", fontSize: 20 }}>›</span>
            <Tag label="SEAT" value={app.assignment.seat}/>
            <span style={{ color: "var(--tx-4)", fontSize: 20 }}>›</span>
            <Tag label="ROLE" value={app.assignment.role}/>
            <span className="flex"/>
            <Tag label="CHANNELS" value="4 auto-tuned" mono/>
          </div>
        </Panel>

        {/* Active mission */}
        <Panel title="◆ ACTIVE MISSION · OP STARWALK" style={{ marginTop: 12 }} accessory={<span className="cap" style={{ color: "var(--ac-ok)" }}>● LIVE · T+1H 24M</span>}>
          <div className="row acenter gap-6">
            <Field label="Mission Lead"><span style={{ color: "var(--tx-0)" }}>FPGElphi</span></Field>
            <Field label="Type"><span style={{ color: "var(--tx-0)" }}>Pirate Sweep</span></Field>
            <Field label="System"><span style={{ color: "var(--tx-0)" }}>STANTON · MICROTECH</span></Field>
            <Field label="Frequency Plan"><span className="mono" style={{ color: "var(--ac-primary)" }}>FP-STANTON-04</span></Field>
            <span className="flex"/>
            <button className="btn"><Icon name="info" size={11}/> BRIEFING</button>
          </div>
        </Panel>

        {/* Fleet roster, grouped by wing */}
        <Panel title="◆ FLEET ROSTER" style={{ marginTop: 12 }} accessory={
          <div className="row gap-3">
            <input className="input" style={{ height: 24, fontSize: 11 }} placeholder="Search ship/pilot…"/>
            <button className="btn btn-sm"><Icon name="list" size={11}/> HIERARCHY</button>
          </div>
        }>
          <div className="col gap-3">
            {WINGS.map(w => (
              <div key={w.name} className="tac">
                <div className="tac-h">
                  <div className="row acenter gap-3">
                    <Icon name="chevronD" size={12} style={{ color: "var(--ac-primary)" }}/>
                    <span className="cap" style={{ color: "var(--tx-0)" }}>{w.name}</span>
                    <span className="cap-dim">· {w.ships.length} SHIPS · LEAD {w.lead}</span>
                  </div>
                  <span className="cap" style={{ color: "var(--ac-primary)" }}>{w.callsign}</span>
                </div>
                <div className="tac-body" style={{ padding: 0 }}>
                  <table className="tbl">
                    <thead><tr><th>Ship</th><th>Class</th><th>Captain</th><th>Crew</th><th>Status</th><th>Channels</th><th></th></tr></thead>
                    <tbody>
                      {w.ships.map(s => (
                        <tr key={s.name}>
                          <td><span style={{ color: "var(--tx-0)" }}>{s.name}</span></td>
                          <td><span className="mono" style={{ fontSize: 10, color: "var(--tx-2)" }}>{s.class}</span></td>
                          <td>{s.captain}</td>
                          <td className="mono">{s.crew}/{s.seats}</td>
                          <td><StatusPill kind={s.status}/></td>
                          <td className="mono" style={{ fontSize: 10 }}>
                            <span style={{ color: "var(--ac-primary)" }}>{s.freqs.join(" · ")}</span>
                          </td>
                          <td>{s.open > 0 && <button className="btn btn-sm">JOIN ({s.open})</button>}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
            ))}
          </div>
        </Panel>
      </div>

      {/* Right: role channels & auto-tuned */}
      <div style={{ borderLeft: "1px solid var(--bd-1)", background: "var(--bg-0)", padding: 16, overflow: "auto" }}>
        <div className="cap" style={{ color: "var(--ac-primary)" }}>ROLE CHANNELS · AUTO-TUNED</div>
        <div className="col gap-3" style={{ marginTop: 10 }}>
          {ROLE_CHANS.map(rc => (
            <Tac key={rc.role}>
              <div className="row between acenter" style={{ marginBottom: 6 }}>
                <span style={{ fontSize: 12, color: "var(--tx-0)" }}>{rc.role}</span>
                <span className="cap" style={{ color: "var(--ac-primary)", background: "var(--bg-1)", border: "1px solid var(--ac-primary-dim)", padding: "1px 5px" }}>ROLE</span>
              </div>
              <div className="mono" style={{ fontSize: 16, color: "var(--ac-lcd)" }}>{rc.freq}</div>
              <div className="row acenter gap-3" style={{ marginTop: 4 }}>
                <span className="cap-dim">{rc.count} CONNECTED</span>
                {rc.active && <span className="cap" style={{ color: "var(--ac-ok)" }}>● {rc.talker}</span>}
              </div>
            </Tac>
          ))}
        </div>

        <div className="cap" style={{ color: "var(--ac-primary)", marginTop: 18 }}>OPEN SEATS</div>
        <div className="col gap-2" style={{ marginTop: 10 }}>
          {OPEN_SEATS.map((o,i) => (
            <div key={i} className="row between acenter" style={{ padding: 8, border: "1px solid var(--bd-2)", borderRadius: 4, background: "var(--bg-2)" }}>
              <div className="col">
                <span style={{ fontSize: 11, color: "var(--tx-0)" }}>{o.ship}</span>
                <span className="cap-dim mono" style={{ fontSize: 9 }}>{o.seat}</span>
              </div>
              <button className="btn btn-sm btn-primary">CLAIM</button>
            </div>
          ))}
        </div>
      </div>

      {shipPicker && <ShipPickerModal onClose={() => setShipPicker(false)} onPick={s => { setApp({ assignment: s }); setShipPicker(false); }}/>}
    </div>
  );
}

function Tag({ label, value, primary, mono }) {
  return (
    <div className="col">
      <span className="field-label">{label}</span>
      <span style={{ fontSize: 14, color: primary ? "var(--ac-primary)" : "var(--tx-0)", fontFamily: mono ? "var(--ff-mono)" : undefined, fontWeight: 500 }}>{value}</span>
    </div>
  );
}

function ShipPickerModal({ onClose, onPick }) {
  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal" onClick={e => e.stopPropagation()} style={{ width: 520 }}>
        <div className="panel-h">
          <span className="cap" style={{ color: "var(--ac-primary)" }}>SHIP PICKER · SELECT SEAT</span>
          <button className="btn btn-ghost btn-icon btn-sm" onClick={onClose}><Icon name="x" size={11}/></button>
        </div>
        <div style={{ padding: 16 }}>
          <input className="input" placeholder="Search ships in roster…" style={{ marginBottom: 12 }}/>
          <div className="col gap-3">
            {ROSTER.map(s => (
              <div key={s.name} className="tac">
                <div className="tac-h">
                  <span style={{ color: "var(--tx-0)", fontSize: 12 }}>{s.name}</span>
                  <span className="cap-dim mono">{s.class} · {s.seats.length} SEATS</span>
                </div>
                <div className="tac-body" style={{ padding: 8, display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(120px, 1fr))", gap: 6 }}>
                  {s.seats.map(st => (
                    <button key={st.name} className="btn btn-sm" style={{ justifyContent: "flex-start", height: 32, padding: "0 10px" }} disabled={st.taken} onClick={() => onPick({ ship: s.name, seat: st.name, role: st.role })}>
                      <span style={{ color: st.taken ? "var(--tx-4)" : "var(--ac-primary)", marginRight: 6 }}>●</span>
                      <span className="col" style={{ alignItems: "flex-start", lineHeight: 1.1 }}>
                        <span>{st.name}</span>
                        <span className="cap-dim" style={{ fontSize: 8 }}>{st.role}</span>
                      </span>
                    </button>
                  ))}
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

const WINGS = [
  {
    name: "DISCOVERY WING", lead: "FPGSchiba", callsign: "DSC-LEAD",
    ships: [
      { name: "ARK-04 Persephone", class: "CARRACK · EXPLR", captain: "FPGSchiba", crew: 4, seats: 5, status: "combat", freqs: ["118.500","127.250","144.000"], open: 1 },
      { name: "Phoenix-Eye", class: "TERRAPIN · RECON", captain: "Dabble", crew: 2, seats: 2, status: "available", freqs: ["118.500","139.500"], open: 0 },
    ],
  },
  {
    name: "SHINOBI WING", lead: "JohnMckeel", callsign: "SHI-LEAD",
    ships: [
      { name: "Black Lance", class: "HORNET · FIGHTER", captain: "Deathtype", crew: 1, seats: 1, status: "combat", freqs: ["122.750","144.000"], open: 0 },
      { name: "Quickfade", class: "GLADIUS · FIGHTER", captain: "I_Die_a_lot", crew: 1, seats: 1, status: "combat", freqs: ["122.750","144.000"], open: 0 },
      { name: "Nightwhisper", class: "SABRE · FIGHTER", captain: "—", crew: 0, seats: 1, status: "afk", freqs: ["122.750"], open: 1 },
    ],
  },
  {
    name: "PHOENIX WING", lead: "FPGElphi", callsign: "PHX-LEAD",
    ships: [
      { name: "Reclaimer-7", class: "RECLAIMER · SLVG", captain: "FPGElphi", crew: 6, seats: 8, status: "discipline", freqs: ["131.500","144.000"], open: 2 },
      { name: "Tide-Walker", class: "CATERPILLAR · CARGO", captain: "Spaceharvest", crew: 3, seats: 4, status: "available", freqs: ["131.500","127.250"], open: 1 },
    ],
  },
];

const ROLE_CHANS = [
  { role: "Engineers · Fleet", freq: "144.000", count: 9, active: true, talker: "FPGElphi" },
  { role: "Pilots · Fleet", freq: "127.250", count: 6, active: false },
  { role: "Gunners · Fleet", freq: "122.750", count: 4, active: false },
  { role: "Medics · Fleet", freq: "139.500", count: 2, active: false },
];

const OPEN_SEATS = [
  { ship: "ARK-04 Persephone", seat: "TURRET-DORSAL · Gunner" },
  { ship: "Nightwhisper", seat: "PILOT · Pilot" },
  { ship: "Reclaimer-7", seat: "ENGINEER-A · Engineer" },
  { ship: "Reclaimer-7", seat: "ENGINEER-B · Engineer" },
  { ship: "Tide-Walker", seat: "CO-PILOT · Co-pilot" },
];

const ROSTER = [
  { name: "ARK-04 Persephone", class: "CARRACK · EXPLR", seats: [
    { name: "PILOT", role: "Pilot", taken: true },
    { name: "CO-PILOT", role: "Co-pilot", taken: true },
    { name: "TURRET-DORSAL", role: "Gunner", taken: false },
    { name: "ENGINEER-A", role: "Engineer", taken: true },
    { name: "MEDIC", role: "Medic", taken: true },
  ]},
  { name: "Quickfade", class: "GLADIUS · FIGHTER", seats: [
    { name: "PILOT", role: "Pilot", taken: true },
  ]},
  { name: "Reclaimer-7", class: "RECLAIMER · SLVG", seats: [
    { name: "PILOT", role: "Pilot", taken: true },
    { name: "CO-PILOT", role: "Co-pilot", taken: true },
    { name: "ENGINEER-A", role: "Engineer", taken: false },
    { name: "ENGINEER-B", role: "Engineer", taken: false },
    { name: "SALVAGE-OP", role: "Salvager", taken: true },
    { name: "TURRET-A", role: "Gunner", taken: true },
    { name: "TURRET-B", role: "Gunner", taken: true },
    { name: "CARGO-MASTER", role: "Logistician", taken: true },
  ]},
];

Object.assign(window, { ScreenShip });
