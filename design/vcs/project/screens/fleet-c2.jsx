/* global React, Icon, StatusPill, Toggle, Panel, Tac, Field, HealthBar, Segmented */
/* ============================================================
   Fleet Mode — C2 dashboard
   ============================================================ */
const { useState: useFleetState } = React;

function ScreenFleet({ app, setApp }) {
  const [selectedShip, setSelectedShip] = useFleetState("ARK-04 Persephone");
  const [wingView, setWingView] = useFleetState(false);
  const ship = FLEET[selectedShip];

  return (
    <div style={{ height: "100%", padding: 12, display: "grid", gridTemplateRows: "auto auto 1fr auto", gap: 10, minHeight: 0 }}>
      {/* Header: mission timer + assignment + view toggle */}
      <div className="row acenter gap-5" style={{ padding: "8px 12px", background: "var(--bg-0)", border: "1px solid var(--bd-1)", borderRadius: 4 }}>
        <div className="row acenter gap-3">
          <Icon name="fleet" size={16} style={{ color: "var(--ac-primary)" }}/>
          <span className="cap" style={{ color: "var(--ac-primary)", fontSize: 11 }}>OP STARWALK · PIRATE SWEEP</span>
          <span className="cap" style={{ color: "var(--ac-ok)" }}>● LIVE</span>
        </div>
        <span className="sep-v" style={{ height: 24 }}/>
        <div className="col">
          <span className="field-label">ELAPSED</span>
          <span className="mono" style={{ fontSize: 18, color: "var(--ac-lcd)", letterSpacing: "0.06em" }}>01:24:18</span>
        </div>
        <div className="col">
          <span className="field-label">NEXT PHASE</span>
          <span className="mono" style={{ fontSize: 14, color: "var(--ac-warn)" }}>T-08:42 · Extract</span>
        </div>
        <span className="sep-v" style={{ height: 24 }}/>
        <div className="col">
          <span className="field-label">FC</span>
          <span style={{ color: "var(--tx-0)" }}>FPGSchiba <span className="cap-dim">(you)</span></span>
        </div>
        <div className="col">
          <span className="field-label">FREQ PLAN</span>
          <span className="mono" style={{ color: "var(--ac-primary)" }}>FP-STANTON-04</span>
        </div>
        <span className="flex"/>
        <Segmented value={wingView ? "wings" : "flat"} onChange={v => setWingView(v === "wings")} options={[
          { value: "flat", label: "FLAT", icon: "list" },
          { value: "wings", label: "WINGS", icon: "fleet" },
        ]}/>
      </div>

      {/* Fleet roster strip */}
      <div className="panel" style={{ overflow: "hidden" }}>
        <div className="panel-h">
          <span className="cap" style={{ color: "var(--tx-1)" }}>FLEET ROSTER · {Object.keys(FLEET).length} SHIPS · {wingView ? "GROUPED BY WING" : "FLAT"}</span>
          <span className="cap-dim">Click a ship to inspect</span>
        </div>
        {wingView ? (
          <WingGrouped fleet={FLEET} selected={selectedShip} setSelected={setSelectedShip}/>
        ) : (
          <div style={{ display: "flex", gap: 8, padding: 10, overflowX: "auto" }}>
            {Object.values(FLEET).map(s => <ShipCard key={s.name} ship={s} selected={selectedShip === s.name} onClick={() => setSelectedShip(s.name)}/>)}
          </div>
        )}
      </div>

      {/* Main 3-column dashboard */}
      <div style={{ display: "grid", gridTemplateColumns: "1.05fr 1fr 1fr", gap: 10, minHeight: 0 }}>
        {/* Column 1: Selected ship detail */}
        <ShipDetail ship={ship}/>

        {/* Column 2: Objectives + Tasking */}
        <div className="col gap-3" style={{ minHeight: 0 }}>
          <Panel title="◆ OBJECTIVES" accessory={<span className="cap-dim mono">3 / 7 COMPLETE</span>} style={{ display: "flex", flexDirection: "column", minHeight: 0 }}>
            <div style={{ maxHeight: 240, overflow: "auto" }}>
              <div className="col gap-2">
                {OBJECTIVES.map((o,i) => (
                  <div key={i} className="row acenter gap-3" style={{ padding: 6, borderBottom: i < OBJECTIVES.length - 1 ? "1px solid var(--bd-1)" : "none" }}>
                    <ObjGlyph state={o.state}/>
                    <div className="col" style={{ flex: 1, minWidth: 0 }}>
                      <span style={{ fontSize: 12, color: o.state === "complete" ? "var(--tx-3)" : "var(--tx-0)", textDecoration: o.state === "complete" ? "line-through" : "none" }}>{o.title}</span>
                      <span className="cap-dim mono" style={{ fontSize: 9 }}>{o.assigned}{o.progress ? ` · ${o.progress}` : ""}</span>
                    </div>
                    {o.note && <span className="mono" style={{ fontSize: 9, color: "var(--tx-4)" }} title={o.note}><Icon name="info" size={10}/></span>}
                  </div>
                ))}
              </div>
            </div>
          </Panel>
          <Panel title="◆ TASKING QUEUE" accessory={<button className="btn btn-sm btn-primary"><Icon name="plus" size={10}/> ISSUE</button>}>
            <div className="col gap-2">
              {TASKING.map((t,i) => (
                <div key={i} className="tac">
                  <div className="row acenter gap-3" style={{ padding: 8 }}>
                    <span className="mono" style={{ fontSize: 9, color: "var(--tx-3)", width: 36 }}>{t.time}</span>
                    <span style={{ fontSize: 12, color: "var(--tx-0)", flex: 1 }}>{t.order}</span>
                    <span className={`pill pill-${t.state === "completed" ? "nominal" : t.state === "acknowledged" ? "discipline" : "afk"}`}>
                      <span className="dot"/>{t.state.toUpperCase()}
                    </span>
                  </div>
                </div>
              ))}
            </div>
          </Panel>
        </div>

        {/* Column 3: Activity + Contacts */}
        <div className="col gap-3" style={{ minHeight: 0 }}>
          <Panel title="◆ COMMS ACTIVITY" accessory={<span className="cap-dim mono">7 CHANNELS</span>}>
            <div className="col gap-2">
              {COMMS_ACTIVITY.map((c,i) => (
                <div key={i} className="row acenter gap-3" style={{ padding: "4px 0" }}>
                  <span style={{ width: 6, height: 6, borderRadius: "50%", background: c.live ? "var(--ac-ok)" : "var(--tx-4)", boxShadow: c.live ? "0 0 4px var(--ac-ok)" : "none", flex: "0 0 6px" }}/>
                  <span className="mono" style={{ fontSize: 11, color: "var(--ac-lcd)", width: 60 }}>{c.freq}</span>
                  <span style={{ fontSize: 11, color: "var(--tx-2)", width: 80, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{c.name}</span>
                  <span className="flex"/>
                  <span style={{ fontSize: 11, color: c.live ? "var(--ac-ok)" : "var(--tx-3)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", maxWidth: 120 }}>{c.live ? `▶ ${c.talker}` : c.last ? `last · ${c.last}` : "—"}</span>
                </div>
              ))}
            </div>
          </Panel>

          <Panel title="◆ CONTACTS · THREATS" accessory={<button className="btn btn-sm"><Icon name="plus" size={10}/> ADD</button>} style={{ minHeight: 0, flex: 1 }}>
            <div className="col gap-2">
              {CONTACTS.map((c,i) => (
                <div key={i} className="tac" style={{ borderLeft: `2px solid ${c.threat === "hostile" ? "var(--ac-alert)" : c.threat === "unknown" ? "var(--ac-warn)" : "var(--ac-primary)"}` }}>
                  <div className="tac-body" style={{ padding: 8 }}>
                    <div className="row between acenter">
                      <span style={{ fontSize: 12, color: "var(--tx-0)" }}>{c.label}</span>
                      <span className={`pill pill-${c.threat === "hostile" ? "critical" : c.threat === "unknown" ? "discipline" : "nominal"}`}>
                        <span className="dot"/>{c.threat.toUpperCase()}
                      </span>
                    </div>
                    <div className="row acenter gap-4" style={{ marginTop: 4, fontFamily: "var(--ff-mono)", fontSize: 10, color: "var(--tx-3)" }}>
                      <span>{c.type}</span>
                      <span>{c.dist}</span>
                      <span>upd {c.upd}</span>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          </Panel>
        </div>
      </div>

      {/* Bottom: Alert controls + Recent alerts */}
      <div style={{ display: "grid", gridTemplateColumns: "1.4fr 1fr", gap: 10 }}>
        <div className="tac">
          <div className="tac-h">
            <span className="cap" style={{ color: "var(--ac-alert)" }}><Icon name="sos" size={11}/> FLEET ALERT CONTROLS · FC ONLY</span>
            <span className="cap-dim">One-tap fleet-wide broadcasts</span>
          </div>
          <div className="row gap-3" style={{ padding: 10 }}>
            {ALERTS.map(a => (
              <button key={a.label} className="btn" style={{ flex: 1, height: 36, color: a.color, borderColor: a.color + "55" }}>
                <Icon name={a.icon} size={12}/> {a.label}
              </button>
            ))}
          </div>
        </div>
        <div className="tac">
          <div className="tac-h">
            <span className="cap">RECENT ALERTS</span>
          </div>
          <div className="col" style={{ padding: 6, fontFamily: "var(--ff-mono)", fontSize: 11 }}>
            {RECENT_ALERTS.map((r,i) => (
              <div key={i} className="row acenter gap-3" style={{ padding: "2px 4px" }}>
                <span style={{ width: 5, height: 5, borderRadius: "50%", background: r.color }}/>
                <span style={{ color: "var(--tx-3)", width: 50 }}>{r.time}</span>
                <span style={{ color: "var(--tx-1)" }}>{r.text}</span>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

function WingGrouped({ fleet, selected, setSelected }) {
  // Define wings; ships without a known wing fall into Unassigned
  const ALPHA = ["ARK-04 Persephone", "Phoenix-Eye"];
  const BRAVO = ["Black Lance", "Quickfade"];
  const CHARLIE = ["Reclaimer-7"];
  // Tide-Walker has no assigned wing in this op
  const groups = [
    { name: "ALPHA WING", lead: "FPGSchiba", members: ALPHA },
    { name: "BRAVO WING", lead: "JohnMckeel", members: BRAVO },
    { name: "CHARLIE WING", lead: "FPGElphi", members: CHARLIE },
    { name: "UNASSIGNED", lead: "—", members: Object.keys(fleet).filter(n => !ALPHA.includes(n) && !BRAVO.includes(n) && !CHARLIE.includes(n)) },
  ];
  const [collapsed, setCollapsed] = useFleetState({});
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 8, padding: 10, overflow: "auto" }}>
      {groups.map(g => {
        const ships = g.members.map(n => fleet[n]).filter(Boolean);
        if (!ships.length) return null;
        const avgHealth = Math.round(ships.reduce((a, s) => a + s.health, 0) / ships.length);
        const isCollapsed = collapsed[g.name];
        return (
          <div key={g.name} style={{ border: "1px solid var(--bd-1)", borderRadius: 3, background: "var(--bg-1)" }}>
            <div className="row acenter gap-4" style={{ padding: "6px 10px", background: "var(--bg-2)", borderBottom: isCollapsed ? "none" : "1px solid var(--bd-1)", cursor: "pointer" }} onClick={() => setCollapsed(c => ({ ...c, [g.name]: !c[g.name] }))}>
              <Icon name={isCollapsed ? "chevron" : "chevronD"} size={11} style={{ color: "var(--ac-primary)" }}/>
              <span className="cap" style={{ color: g.name === "UNASSIGNED" ? "var(--tx-3)" : "var(--ac-primary)" }}>{g.name}</span>
              <span className="cap-dim">{ships.length} SHIPS · LEAD {g.lead}</span>
              <span className="flex"/>
              <span className="cap-dim mono">AVG HEALTH</span>
              <div style={{ width: 80 }}><HealthBar value={avgHealth} state={avgHealth > 70 ? "nominal" : avgHealth > 35 ? "degraded" : "critical"}/></div>
              <span className="mono" style={{ fontSize: 10, color: avgHealth > 70 ? "var(--ac-ok)" : avgHealth > 35 ? "var(--ac-warn)" : "var(--ac-alert)", width: 30, textAlign: "right" }}>{avgHealth}%</span>
            </div>
            {!isCollapsed && (
              <div style={{ display: "flex", gap: 8, padding: 8, overflowX: "auto" }}>
                {ships.map(s => <ShipCard key={s.name} ship={s} selected={selected === s.name} onClick={() => setSelected(s.name)}/>)}
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}

function ShipCard({ ship, selected, onClick }) {
  return (
    <div onClick={onClick} style={{
      flex: "0 0 220px",
      padding: 10,
      border: `1px solid ${selected ? "var(--ac-primary)" : "var(--bd-2)"}`,
      background: selected ? "rgba(96,165,250,0.06)" : "var(--bg-2)",
      borderRadius: 4,
      cursor: "pointer",
      position: "relative",
      boxShadow: selected ? "0 0 0 1px var(--ac-primary-glow)" : "none",
    }}>
      <div className="row between acenter" style={{ marginBottom: 6 }}>
        <div className="row acenter gap-2" style={{ minWidth: 0 }}>
          <Icon name="ship" size={12} style={{ color: "var(--ac-primary)", flex: "0 0 12px" }}/>
          <span style={{ fontSize: 12, color: "var(--tx-0)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{ship.name}</span>
        </div>
        <span className={`pill pill-${ship.threat === "engaged" ? "critical" : ship.threat === "distress" ? "combat" : "nominal"}`} style={{ padding: "1px 6px" }}>
          <span className="dot"/>{ship.threat.toUpperCase().slice(0,4)}
        </span>
      </div>
      <div className="cap-dim mono" style={{ fontSize: 9 }}>{ship.callsign} · CPT {ship.captain}</div>
      <div className="row acenter gap-3" style={{ marginTop: 8 }}>
        {/* mini shield ring */}
        <svg width="36" height="36" viewBox="0 0 36 36">
          <circle cx="18" cy="18" r="14" fill="none" stroke="var(--bd-2)" strokeWidth="1"/>
          {["F","R","B","L"].map((d,i) => {
            const v = ship.shields[d];
            const color = v > 70 ? "var(--ac-ok)" : v > 35 ? "var(--ac-warn)" : "var(--ac-alert)";
            const start = -45 + i * 90, end = start + 86;
            const a1 = (start - 90) * Math.PI / 180, a2 = (end - 90) * Math.PI / 180;
            const x1 = 18 + 14 * Math.cos(a1), y1 = 18 + 14 * Math.sin(a1);
            const x2 = 18 + 14 * Math.cos(a2), y2 = 18 + 14 * Math.sin(a2);
            return <path key={d} d={`M ${x1} ${y1} A 14 14 0 0 1 ${x2} ${y2}`} fill="none" stroke={color} strokeWidth="3"/>;
          })}
        </svg>
        {/* mini power triangle */}
        <svg width="36" height="36" viewBox="0 0 36 36">
          <polygon points="18,4 32,30 4,30" fill="rgba(96,165,250,0.05)" stroke="var(--bd-2)"/>
          <circle cx={18 + (ship.power.w - ship.power.e) * 0.4} cy={18 - (ship.power.s - 60) * 0.3} r="2.5" fill="var(--ac-primary)"/>
        </svg>
        <div className="col" style={{ flex: 1 }}>
          <div className="row between acenter">
            <span className="cap-dim" style={{ fontSize: 9 }}>HEALTH</span>
            <span className="mono" style={{ fontSize: 10, color: ship.health > 70 ? "var(--ac-ok)" : ship.health > 35 ? "var(--ac-warn)" : "var(--ac-alert)" }}>{ship.health}%</span>
          </div>
          <HealthBar value={ship.health} state={ship.health > 70 ? "nominal" : ship.health > 35 ? "degraded" : "critical"}/>
          <div className="row between acenter" style={{ marginTop: 6 }}>
            <span className="cap-dim mono" style={{ fontSize: 9 }}>CREW {ship.crew}/{ship.seats}</span>
            <span style={{ width: 6, height: 6, borderRadius: "50%", background: ship.live ? "var(--ac-ok)" : "var(--tx-4)", boxShadow: ship.live ? "0 0 4px var(--ac-ok)" : "none" }}/>
          </div>
        </div>
      </div>
    </div>
  );
}

function ShipDetail({ ship }) {
  if (!ship) return null;
  return (
    <Panel title={`◆ ${ship.name.toUpperCase()}`} accessory={
      <span className="cap-dim mono">{ship.callsign} · {ship.class}</span>
    } style={{ display: "flex", flexDirection: "column", minHeight: 0 }}>
      <div className="row acenter gap-5" style={{ marginBottom: 10 }}>
        <Field label="Captain"><span style={{ color: "var(--tx-0)" }}>{ship.captain}</span></Field>
        <Field label="Crew"><span className="mono" style={{ color: "var(--tx-0)" }}>{ship.crew}/{ship.seats}</span></Field>
        <Field label="Threat"><StatusPill kind={ship.threat === "engaged" ? "critical" : ship.threat === "distress" ? "combat" : "nominal"} label={ship.threat.toUpperCase()}/></Field>
        <Field label="Health"><span className="mono" style={{ color: ship.health > 70 ? "var(--ac-ok)" : ship.health > 35 ? "var(--ac-warn)" : "var(--ac-alert)" }}>{ship.health}%</span></Field>
      </div>

      <div style={{ display: "grid", gridTemplateRows: "auto auto 1fr", gap: 10, minHeight: 0 }}>
        {/* Component health, categorized */}
        <div className="tac">
          <div className="tac-h"><span className="cap">COMPONENTS</span><span className="cap-dim">read-only mirror</span></div>
          <div style={{ padding: 6, display: "grid", gridTemplateColumns: "1fr 1fr", gap: 4, maxHeight: 160, overflow: "auto" }}>
            {ship.components.map((c,i) => (
              <div key={i} className="row acenter gap-2" style={{ padding: "3px 6px" }}>
                <Icon name={c.icon || "info"} size={10} style={{ color: "var(--tx-3)", flex: "0 0 10px" }}/>
                <span style={{ fontSize: 10, color: "var(--tx-1)", flex: 1, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{c.name}</span>
                <HealthBar value={c.health} state={c.state}/>
                <span className="mono" style={{ fontSize: 9, color: "var(--tx-3)", width: 24, textAlign: "right" }}>{c.health}%</span>
              </div>
            ))}
          </div>
        </div>

        {/* Crew + seat presence */}
        <div className="tac">
          <div className="tac-h"><span className="cap">CREW</span><span className="cap-dim">seat presence</span></div>
          <div style={{ padding: 6, display: "grid", gridTemplateColumns: "1fr 1fr", gap: 4 }}>
            {ship.seats_list.map((s,i) => (
              <div key={i} className="row acenter gap-3" style={{ padding: "4px 6px", border: "1px solid var(--bd-1)", borderRadius: 3 }}>
                <span className="cap-dim mono" style={{ fontSize: 9, width: 70 }}>{s.seat}</span>
                {s.player ? (
                  <>
                    <span style={{ fontSize: 11, color: "var(--tx-0)", flex: 1 }}>{s.player}</span>
                    <StatusPill kind={s.status} mini/>
                  </>
                ) : (
                  <span className="cap-dim" style={{ fontSize: 10, flex: 1, fontStyle: "italic" }}>open</span>
                )}
              </div>
            ))}
          </div>
        </div>

        {/* Damage feed */}
        <div className="tac" style={{ display: "flex", flexDirection: "column", minHeight: 0 }}>
          <div className="tac-h"><span className="cap">DAMAGE FEED</span><span className="cap-dim">last 5</span></div>
          <div style={{ padding: 6, fontFamily: "var(--ff-mono)", fontSize: 10, overflow: "auto", minHeight: 0 }}>
            {ship.damage.map((d,i) => (
              <div key={i} className="row gap-3" style={{ padding: "2px 4px" }}>
                <span style={{ color: "var(--tx-3)" }}>{d.time}</span>
                <span style={{ color: d.state === "critical" ? "var(--ac-alert)" : d.state === "degraded" ? "var(--ac-warn)" : "var(--tx-1)", flex: 1 }}>{d.text}</span>
              </div>
            ))}
          </div>
        </div>
      </div>
    </Panel>
  );
}

function ObjGlyph({ state }) {
  if (state === "complete") return <div style={{ width: 12, height: 12, borderRadius: 2, background: "var(--ac-ok)", display: "grid", placeItems: "center", flex: "0 0 12px" }}><span style={{ color: "#000", fontSize: 8 }}>✓</span></div>;
  if (state === "in progress") return <div style={{ width: 12, height: 12, borderRadius: 2, border: "1px solid var(--ac-primary)", display: "grid", placeItems: "center", flex: "0 0 12px" }}><span style={{ width: 4, height: 4, background: "var(--ac-primary)", boxShadow: "0 0 4px var(--ac-primary)" }}/></div>;
  if (state === "failed") return <div style={{ width: 12, height: 12, borderRadius: 2, background: "var(--ac-alert)", display: "grid", placeItems: "center", flex: "0 0 12px", color: "#fff", fontSize: 9 }}>✕</div>;
  if (state === "aborted") return <div style={{ width: 12, height: 12, borderRadius: 2, background: "var(--tx-4)", flex: "0 0 12px" }}/>;
  return <div style={{ width: 12, height: 12, borderRadius: 2, border: "1px solid var(--bd-2)", flex: "0 0 12px" }}/>;
}

/* ----------------- Data ----------------- */

const FLEET = {
  "ARK-04 Persephone": {
    name: "ARK-04 Persephone", callsign: "PERSE-01", class: "CARRACK · EXPLR", captain: "FPGSchiba",
    crew: 4, seats: 5, health: 64, threat: "engaged", live: true,
    shields: { F: 94, R: 62, L: 78, B: 41 },
    power: { w: 70, s: 90, e: 50 },
    components: [
      { name: "Main Reactor", health: 92, state: "nominal", icon: "bolt" },
      { name: "Aux Reactor", health: 100, state: "nominal", icon: "bolt" },
      { name: "Port Engine", health: 41, state: "degraded", icon: "engine" },
      { name: "Starboard Engine", health: 88, state: "nominal", icon: "engine" },
      { name: "Forward Shield", health: 94, state: "nominal", icon: "shield" },
      { name: "Aft Shield", health: 41, state: "degraded", icon: "shield" },
      { name: "QT Drive", health: 18, state: "critical", icon: "quantum" },
      { name: "Life Support", health: 100, state: "nominal", icon: "air" },
      { name: "Forward Weapon S1", health: 100, state: "nominal", icon: "weapon" },
      { name: "Starboard Turret S2", health: 0, state: "offline", icon: "weapon" },
      { name: "Long-range Sensors", health: 86, state: "nominal", icon: "sensor" },
      { name: "Hydrogen Reserves", health: 72, state: "nominal", icon: "fuel" },
    ],
    seats_list: [
      { seat: "PILOT", player: "FPGSchiba", status: "combat" },
      { seat: "CO-PILOT", player: "Vanderwolf", status: "available" },
      { seat: "TURRET-D", player: null },
      { seat: "ENGINEER-A", player: "Mokushiroku", status: "discipline" },
      { seat: "MEDIC", player: "Dabble", status: "available" },
    ],
    damage: [
      { time: "21:04:12", text: "Port engine 67% → 41% (degraded)", state: "degraded" },
      { time: "21:03:58", text: "Aft shield 78% → 41% (degraded)", state: "degraded" },
      { time: "21:03:30", text: "QT drive spool aborted at 60%", state: "critical" },
      { time: "21:01:14", text: "Stbd turret S2 severed", state: "critical" },
      { time: "20:58:01", text: "Life support CO2 nominal", state: "nominal" },
    ],
  },
  "Reclaimer-7": {
    name: "Reclaimer-7", callsign: "PHX-01", class: "RECLAIMER · SLVG", captain: "FPGElphi",
    crew: 6, seats: 8, health: 88, threat: "safe", live: true,
    shields: { F: 95, R: 92, L: 95, B: 88 }, power: { w: 30, s: 70, e: 50 },
    components: [
      { name: "Main Reactor", health: 100, state: "nominal", icon: "bolt" },
      { name: "Engines (all)", health: 92, state: "nominal", icon: "engine" },
      { name: "Shields (all)", health: 95, state: "nominal", icon: "shield" },
      { name: "Salvage Beam A", health: 82, state: "nominal", icon: "weapon" },
      { name: "Salvage Beam B", health: 78, state: "nominal", icon: "weapon" },
      { name: "Cargo Bay", health: 100, state: "nominal", icon: "cargo" },
      { name: "QT Drive", health: 100, state: "nominal", icon: "quantum" },
      { name: "Life Support", health: 100, state: "nominal", icon: "air" },
    ],
    seats_list: [
      { seat: "PILOT", player: "FPGElphi", status: "discipline" },
      { seat: "CO-PILOT", player: "JohnMckeel", status: "discipline" },
      { seat: "ENGINEER-A", player: "ColdSpoke", status: "afk" },
      { seat: "ENGINEER-B", player: null },
      { seat: "SALVAGE-OP", player: "Spaceharvest", status: "available" },
      { seat: "TURRET-A", player: "I_Die_a_lot", status: "available" },
      { seat: "TURRET-B", player: null },
      { seat: "CARGO-MASTER", player: "Vanderwolf", status: "available" },
    ],
    damage: [
      { time: "20:48:01", text: "Salvage Beam B: thermal warning", state: "degraded" },
      { time: "20:42:14", text: "Cargo Bay: locked for transit", state: "nominal" },
      { time: "20:30:00", text: "Operation start", state: "nominal" },
    ],
  },
  "Quickfade": {
    name: "Quickfade", callsign: "SHI-02", class: "GLADIUS · FIGHTER", captain: "I_Die_a_lot",
    crew: 1, seats: 1, health: 32, threat: "engaged", live: true,
    shields: { F: 60, R: 28, L: 41, B: 14 }, power: { w: 90, s: 30, e: 80 },
    components: [
      { name: "Reactor", health: 86, state: "nominal", icon: "bolt" },
      { name: "Engines", health: 70, state: "nominal", icon: "engine" },
      { name: "Shields", health: 35, state: "degraded", icon: "shield" },
      { name: "S1 Cannon L", health: 100, state: "nominal", icon: "weapon" },
      { name: "S1 Cannon R", health: 0, state: "offline", icon: "weapon" },
      { name: "Sensors", health: 72, state: "nominal", icon: "sensor" },
    ],
    seats_list: [
      { seat: "PILOT", player: "I_Die_a_lot", status: "combat" },
    ],
    damage: [
      { time: "21:11:42", text: "S1 Cannon R severed", state: "critical" },
      { time: "21:10:09", text: "Aft shield collapse → 14%", state: "critical" },
      { time: "21:08:30", text: "Engaged: 2 hostiles", state: "degraded" },
    ],
  },
  "Black Lance": {
    name: "Black Lance", callsign: "SHI-01", class: "HORNET · FIGHTER", captain: "Deathtype",
    crew: 1, seats: 1, health: 78, threat: "engaged", live: true,
    shields: { F: 88, R: 72, L: 88, B: 62 }, power: { w: 90, s: 50, e: 80 },
    components: [
      { name: "Reactor", health: 92, state: "nominal", icon: "bolt" },
      { name: "Engines", health: 88, state: "nominal", icon: "engine" },
      { name: "Shields", health: 78, state: "nominal", icon: "shield" },
      { name: "S2 Cannon", health: 80, state: "nominal", icon: "weapon" },
      { name: "Missile rack", health: 50, state: "degraded", icon: "weapon" },
    ],
    seats_list: [
      { seat: "PILOT", player: "Deathtype", status: "combat" },
    ],
    damage: [
      { time: "21:13:21", text: "Missile rack 2/4 fired", state: "degraded" },
    ],
  },
  "Phoenix-Eye": {
    name: "Phoenix-Eye", callsign: "DSC-02", class: "TERRAPIN · RECON", captain: "Dabble",
    crew: 2, seats: 2, health: 96, threat: "safe", live: true,
    shields: { F: 98, R: 96, L: 96, B: 94 }, power: { w: 20, s: 60, e: 100 },
    components: [
      { name: "Reactor", health: 100, state: "nominal", icon: "bolt" },
      { name: "Engines", health: 100, state: "nominal", icon: "engine" },
      { name: "Long-range Sensors", health: 96, state: "nominal", icon: "sensor" },
      { name: "Shields", health: 96, state: "nominal", icon: "shield" },
    ],
    seats_list: [
      { seat: "PILOT", player: "Dabble", status: "available" },
      { seat: "SCANNER", player: "Mokushiroku", status: "available" },
    ],
    damage: [],
  },
  "Tide-Walker": {
    name: "Tide-Walker", callsign: "PHX-02", class: "CATERPILLAR · CARGO", captain: "Spaceharvest",
    crew: 3, seats: 4, health: 22, threat: "distress", live: true,
    shields: { F: 12, R: 8, L: 0, B: 14 }, power: { w: 0, s: 100, e: 0 },
    components: [
      { name: "Reactor", health: 60, state: "degraded", icon: "bolt" },
      { name: "Engines", health: 8, state: "critical", icon: "engine" },
      { name: "Shields", health: 6, state: "critical", icon: "shield" },
      { name: "Cargo Bay", health: 70, state: "degraded", icon: "cargo" },
      { name: "QT Drive", health: 0, state: "offline", icon: "quantum" },
    ],
    seats_list: [
      { seat: "PILOT", player: "Spaceharvest", status: "combat" },
      { seat: "CO-PILOT", player: null },
      { seat: "ENGINEER", player: null },
      { seat: "CARGO", player: null },
    ],
    damage: [
      { time: "21:15:02", text: "DISTRESS BEACON FIRED", state: "critical" },
      { time: "21:14:38", text: "QT drive offline", state: "critical" },
      { time: "21:14:11", text: "Port shield collapse", state: "critical" },
      { time: "21:13:42", text: "Engaged: 3 hostiles", state: "degraded" },
    ],
  },
};

const OBJECTIVES = [
  { title: "Rendezvous at MICRO-7", state: "complete", assigned: "All ships", progress: "" },
  { title: "Establish defensive perimeter", state: "complete", assigned: "Discovery, Shinobi" },
  { title: "Sweep ASTEROID FIELD-04", state: "in progress", assigned: "Shinobi wing", progress: "3/5 sectors" },
  { title: "Extract intel from disabled vessel", state: "in progress", assigned: "Reclaimer-7", note: "Salvage Beam B thermal warning." },
  { title: "Respond to distress · Tide-Walker", state: "in progress", assigned: "Persephone, Phoenix-Eye", note: "Beacon fired 21:15:02." },
  { title: "Return to base", state: "pending", assigned: "All ships" },
  { title: "Debrief at FOB ALPHA", state: "pending", assigned: "FC + wing leads" },
];

const TASKING = [
  { time: "21:15", order: "Persephone → assist Tide-Walker, MICRO-7 N", state: "acknowledged" },
  { time: "21:14", order: "Phoenix-Eye → dorsal eyes, range 4km", state: "completed" },
  { time: "21:12", order: "Shinobi wing → engage at will", state: "completed" },
  { time: "21:08", order: "Reclaimer-7 → halt salvage, repos", state: "sent" },
  { time: "21:05", order: "Engineers → power 30/70/0 pre-QT", state: "completed" },
];

const COMMS_ACTIVITY = [
  { freq: "118.500", name: "Fleet Common",   live: true, talker: "FPGElphi", last: "" },
  { freq: "122.750", name: "Gunners Net",    live: true, talker: "Deathtype", last: "" },
  { freq: "127.250", name: "Pilots Net",     live: false, last: "Spaceharvest" },
  { freq: "144.000", name: "Engineers Net",  live: true, talker: "ColdSpoke", last: "" },
  { freq: "139.500", name: "Medics Net",     live: false, last: "Mokushiroku" },
  { freq: "31.250",  name: "Persephone IC",  live: false, last: "Vanderwolf" },
  { freq: "121.500", name: "Emergency Guard", live: true, talker: "Spaceharvest", last: "" },
];

const CONTACTS = [
  { label: "PIRATE GROUP · DELTA-3",   type: "GLADIUS × 3",     dist: "4.2km", upd: "21:15", threat: "hostile" },
  { label: "PIRATE GROUP · DELTA-1",   type: "CUTLASS × 2",     dist: "6.8km", upd: "21:13", threat: "hostile" },
  { label: "UNKNOWN SIGNAL · BRAVO",   type: "UNCLASSIFIED",    dist: "12.4km", upd: "21:09", threat: "unknown" },
  { label: "ALLIED · VANGUARD WING-4", type: "AVENGER × 1",     dist: "8.1km", upd: "21:14", threat: "friendly" },
];

const ALERTS = [
  { label: "DISTRESS",   icon: "sos",       color: "var(--ac-alert)" },
  { label: "REGROUP",    icon: "fleet",     color: "var(--ac-warn)" },
  { label: "RTB",        icon: "ship",      color: "var(--ac-primary)" },
  { label: "ENGAGE",     icon: "weapon",    color: "var(--ac-alert)" },
  { label: "HOLD",       icon: "lock",      color: "var(--ac-warn)" },
];

const RECENT_ALERTS = [
  { time: "21:15", text: "DISTRESS · Tide-Walker @ MICRO-7", color: "var(--ac-alert)" },
  { time: "21:08", text: "ENGAGE · all wings", color: "var(--ac-alert)" },
  { time: "20:42", text: "REGROUP · MICRO-7 N", color: "var(--ac-warn)" },
  { time: "20:30", text: "OP START · STARWALK", color: "var(--ac-primary)" },
];

Object.assign(window, { ScreenFleet, FLEET });
