/* global React, Icon, StatusPill, Toggle, Tac, Panel, Field, StateCard, Segmented, KeyChip */
/* ============================================================
   Player List, Server Details, Administration
   ============================================================ */
const { useState: usePlrState } = React;

/* ----------------- Player List ----------------- */
function ScreenPlayers({ app, setApp }) {
  const [view, setView] = usePlrState("table");
  const [filter, setFilter] = usePlrState("all");
  const [hover, setHover] = usePlrState(null);
  return (
    <div style={{ padding: 16, height: "100%", overflow: "auto" }}>
      {/* Status presence widget (self) */}
      <Panel title="◆ MY PRESENCE" accessory={
        <span className="cap-dim mono">FFID {app.user.ffid}</span>
      }>
        <div className="row acenter gap-5">
          <div className="col">
            <span className="field-label">CALLSIGN</span>
            <span style={{ fontSize: 14, color: "var(--tx-0)" }}>{app.user.callsign}</span>
          </div>
          <span className="sep-v" style={{ height: 32 }}/>
          <div className="col">
            <span className="field-label">STATUS</span>
            <div className="row gap-3">
              {["available","combat","discipline","afk"].map(s => (
                <button key={s} className={`btn btn-sm ${app.user.status === s ? "btn-primary" : ""}`} onClick={() => setApp({ user: { ...app.user, status: s } })}>
                  <StatusPill kind={s}/>
                </button>
              ))}
            </div>
          </div>
          <span className="flex"/>
          <button className="btn">QUICK-STATUS HOTKEYS…</button>
        </div>
      </Panel>

      {/* filter bar */}
      <div className="row acenter gap-4" style={{ margin: "16px 0 10px" }}>
        <input className="input" placeholder="Search callsign or FFID…" style={{ width: 280 }}/>
        <Segmented value={filter} onChange={setFilter} options={[
          { value: "all", label: "ALL" },
          { value: "team", label: "TEAM" },
          { value: "ship", label: "SHIP" },
          { value: "role", label: "ROLE" },
        ]}/>
        <span className="flex"/>
        <Segmented value={view} onChange={setView} options={[
          { value: "table", label: "TABLE", icon: "list" },
          { value: "hier", label: "HIERARCHY", icon: "fleet" },
        ]}/>
      </div>

      {view === "table" ? (
        <div className="panel" style={{ overflow: "hidden" }}>
          <table className="tbl">
            <thead>
              <tr>
                <th style={{ width: 100 }}>FFID</th>
                <th>Callsign</th>
                <th>Ship · Seat</th>
                <th>Team</th>
                <th>Role</th>
                <th>Status</th>
                <th>Rec</th>
                <th style={{ width: 180 }}>Volume</th>
                <th style={{ width: 80 }}></th>
              </tr>
            </thead>
            <tbody>
              {PLAYERS.map(p => (
                <tr key={p.ffid} onMouseEnter={() => setHover(p.ffid)} onMouseLeave={() => setHover(null)}>
                  <td className="mono" style={{ color: "var(--tx-3)" }}>{p.ffid}</td>
                  <td><span style={{ color: p.self ? "var(--ac-primary)" : "var(--tx-0)" }}>{p.callsign}</span>{p.self && <span className="cap-dim" style={{ marginLeft: 6 }}>(you)</span>}</td>
                  <td><span className="mono" style={{ fontSize: 11 }}>{p.ship}</span><span className="cap-dim" style={{ marginLeft: 6 }}>{p.seat}</span></td>
                  <td><span className="cap" style={{ color: "var(--ac-primary)" }}>{p.team}</span></td>
                  <td>{p.role}</td>
                  <td><StatusPill kind={p.status}/></td>
                  <td>{p.rec ? <Icon name="broadcast" size={12} style={{ color: "var(--ac-alert)" }}/> : <span style={{ color: "var(--tx-4)" }}>—</span>}</td>
                  <td>
                    <div className="row acenter gap-3">
                      <input type="range" min="0" max="100" defaultValue={p.vol} style={{ flex: 1 }}/>
                      <span className="mono" style={{ fontSize: 10, color: "var(--tx-3)", width: 26, textAlign: "right" }}>{p.vol}</span>
                    </div>
                  </td>
                  <td>
                    {hover === p.ffid && !p.self && (
                      <div className="row gap-2">
                        <button className="btn btn-ghost btn-icon btn-sm" title="Whisper"><Icon name="chat" size={11}/></button>
                        <button className="btn btn-ghost btn-icon btn-sm" title="Mute"><Icon name="micOff" size={11}/></button>
                      </div>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <PlayerHierarchy/>
      )}
    </div>
  );
}

function PlayerHierarchy() {
  return (
    <div className="col gap-3">
      {["ARK-04 Persephone","Reclaimer-7","Tide-Walker","Quickfade","Black Lance"].map(ship => (
        <div key={ship} className="tac">
          <div className="tac-h">
            <div className="row acenter gap-3">
              <Icon name="chevronD" size={12} style={{ color: "var(--ac-primary)" }}/>
              <Icon name="ship" size={12} style={{ color: "var(--ac-primary)" }}/>
              <span style={{ color: "var(--tx-0)", fontSize: 12 }}>{ship}</span>
            </div>
            <span className="cap-dim mono">{PLAYERS.filter(p => p.ship === ship).length} CREW</span>
          </div>
          <div className="tac-body" style={{ padding: 0 }}>
            {PLAYERS.filter(p => p.ship === ship).map(p => (
              <div key={p.ffid} className="row acenter gap-4" style={{ padding: "6px 14px 6px 32px", borderTop: "1px solid var(--bd-1)" }}>
                <Icon name="chevron" size={10} style={{ color: "var(--tx-4)" }}/>
                <span className="mono" style={{ fontSize: 10, color: "var(--tx-3)" }}>{p.seat}</span>
                <span style={{ color: "var(--tx-0)", fontSize: 12 }}>{p.callsign}</span>
                <span className="cap-dim">{p.role}</span>
                <span className="flex"/>
                <StatusPill kind={p.status}/>
              </div>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

const PLAYERS = [
  { ffid: "VG-0271", callsign: "FPGSchiba", ship: "ARK-04 Persephone", seat: "PILOT", team: "Discovery", role: "Pilot", status: "combat", rec: false, vol: 80, self: true },
  { ffid: "VG-0188", callsign: "FPGElphi", ship: "Reclaimer-7", seat: "CAPTAIN", team: "Phoenix", role: "Engineer", status: "discipline", rec: true, vol: 75 },
  { ffid: "VG-0094", callsign: "Spaceharvest", ship: "Tide-Walker", seat: "PILOT", team: "Phoenix", role: "Pilot", status: "available", rec: false, vol: 60 },
  { ffid: "VG-0303", callsign: "I_Die_a_lot", ship: "Quickfade", seat: "PILOT", team: "Shinobi", role: "Pilot", status: "combat", rec: false, vol: 65 },
  { ffid: "VG-0419", callsign: "Dabble", ship: "Phoenix-Eye", seat: "PILOT", team: "Discovery", role: "Recon", status: "available", rec: false, vol: 90 },
  { ffid: "VG-0028", callsign: "Deathtype", ship: "Black Lance", seat: "PILOT", team: "Shinobi", role: "Pilot", status: "combat", rec: false, vol: 70 },
  { ffid: "VG-0150", callsign: "JohnMckeel", ship: "Black Lance", seat: "WINGMAN", team: "Shinobi", role: "Fleet Commander", status: "discipline", rec: true, vol: 100 },
  { ffid: "VG-0512", callsign: "Vanderwolf", ship: "ARK-04 Persephone", seat: "ENGINEER-A", team: "Discovery", role: "Engineer", status: "available", rec: false, vol: 55 },
  { ffid: "VG-0233", callsign: "Mokushiroku", ship: "ARK-04 Persephone", seat: "MEDIC", team: "Discovery", role: "Medic", status: "available", rec: false, vol: 70 },
  { ffid: "VG-0644", callsign: "ColdSpoke", ship: "Reclaimer-7", seat: "ENGINEER-B", team: "Phoenix", role: "Engineer", status: "afk", rec: false, vol: 0 },
];

/* ----------------- Server Details ----------------- */
function ScreenServer({ app, setApp }) {
  const [s, setS] = usePlrState({
    coalitionSec: true,
    spectatorAudio: false,
    los: true,
    distance: true,
    irlTx: false,
    irlBeh: false,
    radioExp: true,
    extAwacs: false,
    allowEnc: true,
    strictEnc: true,
    showTuned: true,
    showTxName: true,
  });
  const t = (k, label, desc) => (
    <div className="row acenter between" style={{ padding: "12px 0", borderBottom: "1px solid var(--bd-1)" }}>
      <div className="col">
        <span style={{ fontSize: 13, color: "var(--tx-0)" }}>{label}</span>
        <span className="cap-dim" style={{ fontSize: 10, marginTop: 2, letterSpacing: "0.06em", textTransform: "none" }}>{desc}</span>
      </div>
      <Toggle on={s[k]} onChange={v => setS({ ...s, [k]: v })} lg/>
    </div>
  );
  return (
    <div style={{ padding: 16, height: "100%", overflow: "auto" }}>
      <Panel title="◆ SERVER CONFIGURATION" accessory={<span className="cap-dim mono">srs.vanguard.org:5002 · v3.2.1 · {app.server.region}</span>}>
        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 0 }}>
          <div style={{ padding: "0 16px", borderRight: "1px solid var(--bd-1)" }}>
            <div className="cap" style={{ padding: "12px 0 8px", color: "var(--ac-primary)" }}>SIMULATION REALISM</div>
            {t("coalitionSec", "Coalition Security", "Restrict comms to friendly coalition only.")}
            {t("spectatorAudio", "Spectator Audio", "Spectators may hear all unencrypted traffic.")}
            {t("los", "Line of Sight", "Block signal when line-of-sight obstructed.")}
            {t("distance", "Distance Limitations", "Range falloff per radio band.")}
            {t("irlTx", "IRL Radio Tx Behavior", "Realistic transmit collisions and squelch tails.")}
            {t("irlBeh", "IRL Radio Behavior", "Authentic noise, fade, and capture effect.")}
          </div>
          <div style={{ padding: "0 16px" }}>
            <div className="cap" style={{ padding: "12px 0 8px", color: "var(--ac-primary)" }}>FEATURES & DISPLAY</div>
            {t("radioExp", "Radio Expansion", "Allow > 10 radios per client.")}
            {t("extAwacs", "External AWACS Mode", "External tactical observers may monitor.")}
            {t("allowEnc", "Allow Radio Encryption", "Encryption keys available to all teams.")}
            {t("strictEnc", "Strict Radio Encryption", "Mismatched keys produce static instead of clear audio.")}
            {t("showTuned", "Show Tuned Client Count", "Display number tuned to each frequency.")}
            {t("showTxName", "Show Transmitter Name", "Reveal caller callsign on transmit.")}
          </div>
        </div>
      </Panel>

      <div className="row gap-4" style={{ marginTop: 12, fontFamily: "var(--ff-mono)", fontSize: 11, color: "var(--tx-3)" }}>
        <span>SERVER VERSION <span style={{ color: "var(--tx-0)" }}>v3.2.1-stable</span></span>
        <span className="sep-v" style={{ height: 14 }}/>
        <span>RETRANSMIT NODE LIMIT <span style={{ color: "var(--tx-0)" }}>8</span></span>
        <span className="sep-v" style={{ height: 14 }}/>
        <span>ENCRYPTION KEYS ALLOCATED <span style={{ color: "var(--tx-0)" }}>252 / 256</span></span>
        <span className="flex"/>
        <button className="btn btn-sm">EXPORT CONFIG</button>
      </div>
    </div>
  );
}

/* ----------------- Administration ----------------- */
function ScreenAdmin({ app, setApp }) {
  const [tab, setTab] = usePlrState("players");
  return (
    <div style={{ height: "100%", display: "flex", flexDirection: "column", minHeight: 0 }}>
      <div className="tabs" style={{ padding: "0 16px", background: "var(--bg-0)" }}>
        <span className={`tab ${tab === "players" ? "active" : ""}`} onClick={() => setTab("players")}>Player List</span>
        <span className={`tab ${tab === "freqs" ? "active" : ""}`} onClick={() => setTab("freqs")}>Frequencies</span>
        <span className={`tab ${tab === "server" ? "active" : ""}`} onClick={() => setTab("server")}>Server</span>
        <span className={`tab ${tab === "ships" ? "active" : ""}`} onClick={() => setTab("ships")}>Ships & Roles</span>
        <span className={`tab ${tab === "missions" ? "active" : ""}`} onClick={() => setTab("missions")}>Mission Templates</span>
        <span className="flex"/>
        <span className="cap" style={{ alignSelf: "center", padding: "0 16px", color: "var(--ac-warn)" }}><Icon name="shield" size={11}/> OFFICER ACCESS</span>
      </div>
      <div style={{ flex: 1, overflow: "auto", padding: 16, minHeight: 0 }}>
        {tab === "players" && <AdminPlayers/>}
        {tab === "freqs" && <AdminFreqs/>}
        {tab === "server" && <AdminServer/>}
        {tab === "ships" && <AdminShips/>}
        {tab === "missions" && <AdminMissions setApp={setApp}/>}
      </div>
    </div>
  );
}

function AdminPlayers() {
  return (
    <div>
      <div className="row gap-4 acenter" style={{ marginBottom: 10 }}>
        <input className="input" placeholder="Search players…" style={{ width: 280 }}/>
        <Segmented value="all" onChange={() => {}} options={[
          { value: "all", label: "ALL" },
          { value: "admins", label: "ADMINS" },
          { value: "members", label: "MEMBERS" },
          { value: "guests", label: "GUESTS" },
          { value: "banned", label: "BANNED" },
        ]}/>
      </div>
      <div className="panel">
        <table className="tbl">
          <thead><tr><th>Type / Role</th><th>Callsign</th><th>FFID</th><th>Connected</th><th>Actions</th></tr></thead>
          <tbody>
            {[
              { type: "ADMIN", role: "var(--ac-alert)", name: "FPGSchiba", ffid: "VG-0271", since: "01:14:22" },
              { type: "OFFICER", role: "var(--ac-warn)", name: "FPGElphi", ffid: "VG-0188", since: "00:48:01" },
              { type: "MEMBER", role: "var(--ac-primary)", name: "Spaceharvest", ffid: "VG-0094", since: "00:32:18" },
              { type: "MEMBER", role: "var(--ac-primary)", name: "I_Die_a_lot", ffid: "VG-0303", since: "00:25:00" },
              { type: "GUEST", role: "var(--tx-3)", name: "RogueTango_07", ffid: "—", since: "00:11:42" },
            ].map(r => (
              <tr key={r.name}>
                <td><span className="cap" style={{ color: r.role, border: `1px solid ${r.role}55`, padding: "1px 6px", borderRadius: 2 }}>{r.type}</span></td>
                <td><span style={{ color: "var(--tx-0)" }}>{r.name}</span></td>
                <td className="mono">{r.ffid}</td>
                <td className="mono" style={{ color: "var(--tx-3)" }}>{r.since}</td>
                <td>
                  <div className="row gap-2">
                    <button className="btn btn-sm">KICK</button>
                    <button className="btn btn-sm">MUTE</button>
                    <button className="btn btn-sm btn-danger">BAN</button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function AdminFreqs() {
  return (
    <div className="col gap-4">
      <div className="row acenter gap-4">
        <span className="cap">TEAM</span>
        <select className="input" style={{ width: 200 }}><option>Discovery</option><option>Shinobi</option><option>Phoenix</option><option>All</option></select>
        <button className="btn btn-primary"><Icon name="plus" size={11}/> ADD FREQUENCY</button>
      </div>
      {[
        { freq: "118.500", name: "Fleet Common", enc: false, tuned: ["FPGSchiba","FPGElphi","Dabble","Spaceharvest","JohnMckeel","Vanderwolf","Mokushiroku"] },
        { freq: "122.750", name: "Gunners Net", enc: true, tuned: ["I_Die_a_lot","Deathtype"] },
        { freq: "127.250", name: "Pilots Net", enc: false, tuned: ["FPGSchiba","Spaceharvest","Dabble"] },
        { freq: "144.000", name: "Engineers Net", enc: true, tuned: ["FPGElphi","Vanderwolf","ColdSpoke"] },
        { freq: "139.500", name: "Medics Net", enc: false, tuned: ["Mokushiroku"] },
      ].map(f => (
        <div key={f.freq} className="tac">
          <div className="tac-h">
            <div className="row acenter gap-4">
              <Icon name="chevronD" size={12} style={{ color: "var(--ac-primary)" }}/>
              <span className="mono" style={{ fontSize: 14, color: "var(--ac-lcd)", textShadow: "0 0 4px var(--ac-lcd-dim)" }}>{f.freq}</span>
              <span style={{ color: "var(--tx-0)" }}>{f.name}</span>
              {f.enc && <span className="cap" style={{ color: "var(--ac-warn)" }}><Icon name="lock" size={10}/> ENC</span>}
              <span className="cap-dim mono">{f.tuned.length} TUNED</span>
            </div>
            <div className="row gap-2">
              <button className="btn btn-sm"><Icon name="edit" size={10}/></button>
              <button className="btn btn-sm btn-danger"><Icon name="trash" size={10}/></button>
            </div>
          </div>
          <div className="tac-body" style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
            {f.tuned.map(c => (
              <span key={c} className="pill" style={{ background: "var(--bg-1)" }}>{c}</span>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

function AdminServer() {
  return (
    <div>
      <Panel title="TEAMS">
        <div className="col gap-3">
          {[
            { name: "Discovery", color: "var(--ac-primary)", count: 12, pw: true },
            { name: "Shinobi", color: "#a78bfa", count: 8, pw: true },
            { name: "Phoenix", color: "var(--ac-warn)", count: 6, pw: true },
            { name: "Observers", color: "var(--tx-3)", count: 2, pw: false },
          ].map(t => (
            <div key={t.name} className="row acenter gap-4" style={{ padding: 10, border: "1px solid var(--bd-2)", borderRadius: 4, background: "var(--bg-2)" }}>
              <span style={{ width: 8, height: 8, background: t.color, borderRadius: 2, boxShadow: `0 0 4px ${t.color}` }}/>
              <span style={{ color: "var(--tx-0)", fontWeight: 500 }}>{t.name}</span>
              <span className="cap-dim">{t.count} MEMBERS</span>
              <span className="cap" style={{ color: t.pw ? "var(--ac-warn)" : "var(--tx-3)" }}><Icon name={t.pw ? "lock" : "unlock"} size={10}/> {t.pw ? "PASSWORD" : "OPEN"}</span>
              <span className="flex"/>
              <button className="btn btn-sm"><Icon name="edit" size={10}/></button>
              <button className="btn btn-sm btn-danger"><Icon name="trash" size={10}/></button>
            </div>
          ))}
          <button className="btn"><Icon name="plus" size={11}/> ADD TEAM</button>
        </div>
      </Panel>
      <Panel title="GLOBAL POLICIES" style={{ marginTop: 12 }}>
        <div className="col gap-4">
          {[
            ["Encryption", "Allow encryption on radios", true],
            ["Show transmitter name", "Display caller callsign on transmit", true],
            ["Show tuned client count", "Display tuned count per frequency", true],
          ].map(([l, d, on]) => (
            <div key={l} className="row between acenter">
              <div className="col"><span>{l}</span><span className="cap-dim" style={{ textTransform: "none", letterSpacing: "0.04em" }}>{d}</span></div>
              <Toggle on={on} onChange={() => {}} lg/>
            </div>
          ))}
        </div>
      </Panel>
    </div>
  );
}

function AdminShips() {
  const [selected, setSelected] = usePlrState("ARK-04 Persephone");
  const ship = SHIP_ROSTER.find(s => s.name === selected);
  return (
    <div style={{ display: "grid", gridTemplateColumns: "340px 1fr 280px", gap: 12, height: "100%", minHeight: 0 }}>
      {/* Ships list */}
      <div>
        <div className="row between acenter" style={{ marginBottom: 8 }}>
          <span className="cap">SHIP ROSTER</span>
          <button className="btn btn-sm btn-primary"><Icon name="plus" size={10}/> ADD</button>
        </div>
        <div className="col gap-2">
          {SHIP_ROSTER.map(s => (
            <div
              key={s.name}
              onClick={() => setSelected(s.name)}
              style={{
                padding: 10, border: "1px solid var(--bd-2)", borderRadius: 4,
                background: selected === s.name ? "rgba(96,165,250,0.08)" : "var(--bg-2)",
                borderLeft: selected === s.name ? "2px solid var(--ac-primary)" : "1px solid var(--bd-2)",
                cursor: "pointer",
              }}
            >
              <div className="row between acenter">
                <span style={{ color: "var(--tx-0)", fontWeight: 500 }}>{s.name}</span>
                <span className="cap-dim mono">{s.seats.length} SEATS</span>
              </div>
              <div className="row acenter gap-3" style={{ marginTop: 4 }}>
                <span className="cap-dim mono">{s.class}</span>
              </div>
            </div>
          ))}
        </div>
      </div>

      {/* Seat detail */}
      <Panel title={`◆ ${ship.name.toUpperCase()} · SEATS`} accessory={
        <div className="row gap-2"><button className="btn btn-sm"><Icon name="edit" size={10}/></button><button className="btn btn-sm btn-danger"><Icon name="trash" size={10}/></button></div>
      }>
        <div className="row acenter gap-6" style={{ marginBottom: 12 }}>
          <Field label="Class"><span style={{ color: "var(--tx-0)" }}>{ship.class}</span></Field>
          <Field label="Crew"><span className="mono" style={{ color: "var(--tx-0)" }}>{ship.seats.length}</span></Field>
          <Field label="Image">
            <div style={{ width: 60, height: 32, background: "repeating-linear-gradient(45deg, var(--bg-3), var(--bg-3) 4px, var(--bg-2) 4px, var(--bg-2) 8px)", border: "1px solid var(--bd-2)", borderRadius: 2 }}/>
          </Field>
          <span className="flex"/>
          <button className="btn btn-sm"><Icon name="plus" size={10}/> ADD SEAT</button>
        </div>
        <table className="tbl">
          <thead><tr><th style={{ width: 30 }}>#</th><th>Seat</th><th>Role</th><th>Default Channels</th><th></th></tr></thead>
          <tbody>
            {ship.seats.map((s, i) => (
              <tr key={i}>
                <td className="mono" style={{ color: "var(--tx-3)" }}>
                  <span className="row acenter gap-3"><Icon name="drag" size={11} style={{ color: "var(--tx-4)" }}/>{i + 1}</span>
                </td>
                <td><span style={{ color: "var(--tx-0)" }}>{s.name}</span></td>
                <td>
                  <select className="input" style={{ height: 22, fontSize: 11, padding: "0 6px", minWidth: 120 }} defaultValue={s.role}>
                    {ROLES.map(r => <option key={r}>{r}</option>)}
                  </select>
                </td>
                <td>
                  <div className="row gap-2">
                    {["Intercom","Role","Wing","Fleet"].map(c => (
                      <span key={c} className={`pill ${s.chans.includes(c) ? "pill-nominal" : ""}`} style={{ cursor: "pointer", padding: "1px 6px" }}>{c}</span>
                    ))}
                  </div>
                </td>
                <td><button className="btn btn-ghost btn-icon btn-sm"><Icon name="x" size={10}/></button></td>
              </tr>
            ))}
          </tbody>
        </table>
      </Panel>

      {/* Role catalog */}
      <Panel title="ROLE CATALOG">
        <div className="col gap-2">
          {ROLES.map(r => (
            <div key={r} className="row acenter between" style={{ padding: "4px 8px", border: "1px solid var(--bd-1)", borderRadius: 3, background: "var(--bg-2)" }}>
              <span style={{ fontSize: 12 }}>{r}</span>
              <span className="row gap-2">
                <button className="btn btn-ghost btn-icon btn-sm"><Icon name="edit" size={10}/></button>
                <button className="btn btn-ghost btn-icon btn-sm"><Icon name="x" size={10}/></button>
              </span>
            </div>
          ))}
          <button className="btn btn-sm" style={{ marginTop: 6 }}><Icon name="plus" size={10}/> NEW ROLE</button>
        </div>
      </Panel>
    </div>
  );
}

function AdminMissions({ setApp }) {
  const [selected, setSelected] = usePlrState("STANTON · Pirate Sweep");
  const templates = [
    { name: "STANTON · Pirate Sweep", type: "Combat", ships: 4, duration: "2h 30m", author: "FPGSchiba", file: "stanton-pirate.tpl", modified: "2026-05-12" },
    { name: "STANTON · Salvage Run", type: "Salvage", ships: 2, duration: "1h 45m", author: "FPGElphi", file: "stanton-salvage.tpl", modified: "2026-05-10" },
    { name: "STANTON · Bounty Hunt", type: "Combat", ships: 6, duration: "3h 00m", author: "JohnMckeel", file: "stanton-bounty.tpl", modified: "2026-04-22" },
    { name: "PYRO · Cargo Convoy", type: "Logistics", ships: 8, duration: "4h 30m", author: "Spaceharvest", file: "pyro-convoy.tpl", modified: "2026-04-18" },
    { name: "STANTON · Medical Run", type: "Medical", ships: 2, duration: "1h 15m", author: "Mokushiroku", file: "stanton-medical.tpl", modified: "2026-04-10" },
    { name: "STANTON · Outer Recon", type: "Exploration", ships: 3, duration: "2h 00m", author: "Dabble", file: "stanton-recon.tpl", modified: "2026-03-28" },
  ];
  const sel = templates.find(t => t.name === selected) || templates[0];
  return (
    <div style={{ display: "grid", gridTemplateColumns: "340px 1fr", gap: 12, height: "100%", minHeight: 0 }}>
      <div>
        <div className="row between acenter" style={{ marginBottom: 8 }}>
          <span className="cap">MISSION TEMPLATES</span>
          <span className="cap-dim mono">read-only</span>
        </div>
        <input className="input" placeholder="Filter templates…" style={{ marginBottom: 8 }}/>
        <div className="col gap-2">
          {templates.map(m => (
            <div key={m.name} onClick={() => setSelected(m.name)} style={{
              padding: 10, border: "1px solid var(--bd-2)",
              borderLeft: selected === m.name ? "2px solid var(--ac-primary)" : "1px solid var(--bd-2)",
              background: selected === m.name ? "rgba(96,165,250,0.06)" : "var(--bg-2)",
              borderRadius: 4, cursor: "pointer",
            }}>
              <div className="row between acenter">
                <span style={{ color: "var(--tx-0)" }}>{m.name}</span>
                <span className="cap" style={{ color: "var(--ac-primary)" }}>{m.type.toUpperCase()}</span>
              </div>
              <div className="cap-dim mono" style={{ marginTop: 4 }}>{m.ships} SHIPS · {m.duration} · {m.author}</div>
            </div>
          ))}
        </div>
      </div>
      <div className="col gap-4" style={{ overflow: "auto" }}>
        <Panel title={`◆ ${sel.name.toUpperCase()}`} accessory={
          <div className="row gap-3 acenter">
            <span className="cap-dim mono">{sel.file} · modified {sel.modified}</span>
            <button className="btn btn-sm" disabled title="Template management has moved to the Vanguard org portal">
              <Icon name="lock" size={10}/> MANAGED ELSEWHERE
            </button>
          </div>
        }>
          <div className="row gap-6" style={{ marginBottom: 12 }}>
            <Field label="Category"><span className="cap" style={{ color: "var(--ac-primary)" }}>{sel.type.toUpperCase()}</span></Field>
            <Field label="Ships"><span className="mono" style={{ color: "var(--tx-0)" }}>{sel.ships}</span></Field>
            <Field label="Est. Duration"><span className="mono" style={{ color: "var(--tx-0)" }}>{sel.duration}</span></Field>
            <Field label="Author"><span style={{ color: "var(--tx-0)" }}>{sel.author}</span></Field>
          </div>
          <div className="cap" style={{ color: "var(--ac-primary)", marginBottom: 6 }}>FREQUENCY PLAN</div>
          <table className="tbl" style={{ marginBottom: 16 }}>
            <thead><tr><th>Label</th><th>Frequency</th><th>Enc</th><th>Team</th></tr></thead>
            <tbody>
              {[
                ["Fleet Common", "118.500", false, "All"],
                ["Discovery Wing", "122.750", true, "Discovery"],
                ["Engineers", "144.000", true, "All"],
                ["Gunners", "127.250", true, "All"],
                ["Medics", "139.500", false, "All"],
              ].map(([l,f,e,t]) => (
                <tr key={l}>
                  <td>{l}</td>
                  <td className="mono" style={{ color: "var(--ac-lcd)" }}>{f}</td>
                  <td>{e ? <Icon name="lock" size={11} style={{ color: "var(--ac-warn)" }}/> : <span style={{ color: "var(--tx-4)" }}>—</span>}</td>
                  <td className="cap" style={{ color: "var(--ac-primary)" }}>{t}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </Panel>

        <div className="state-card" style={{ padding: 24 }}>
          <div className="state-glyph"><Icon name="lock" size={20}/></div>
          <div className="state-title">CREATION HANDLED EXTERNALLY</div>
          <div style={{ fontSize: 12, color: "var(--tx-3)", maxWidth: 480, textWrap: "pretty" }}>
            Templates and one-off operations are created and edited in the Vanguard org portal — not in this client. To run a published mission, browse <b style={{ color: "var(--tx-1)" }}>Operations</b>; to schedule a new one, use the portal.
          </div>
          <button className="btn btn-primary" style={{ marginTop: 8 }} onClick={() => setApp && setApp({ view: "operations" })}>
            <Icon name="fleet" size={11}/> BROWSE OPERATIONS
          </button>
        </div>
      </div>
    </div>
  );
}

const ROLES = ["Pilot","Co-pilot","Gunner","Engineer","Medic","Turret Operator","Marine","Recon","Salvager","Logistician","Fleet Commander"];

const SHIP_ROSTER = [
  { name: "ARK-04 Persephone", class: "CARRACK · LARGE", seats: [
    { name: "PILOT", role: "Pilot", chans: ["Intercom","Role","Wing","Fleet"] },
    { name: "CO-PILOT", role: "Co-pilot", chans: ["Intercom","Role","Wing"] },
    { name: "TURRET-DORSAL", role: "Gunner", chans: ["Intercom","Role"] },
    { name: "ENGINEER-A", role: "Engineer", chans: ["Intercom","Role","Fleet"] },
    { name: "MEDIC", role: "Medic", chans: ["Intercom","Role","Fleet"] },
  ]},
  { name: "Phoenix-Eye", class: "TERRAPIN · MEDIUM", seats: [
    { name: "PILOT", role: "Pilot", chans: ["Intercom","Role","Wing","Fleet"] },
    { name: "SCANNER", role: "Recon", chans: ["Intercom","Role"] },
  ]},
  { name: "Quickfade", class: "GLADIUS · SMALL", seats: [
    { name: "PILOT", role: "Pilot", chans: ["Role","Wing","Fleet"] },
  ]},
  { name: "Reclaimer-7", class: "RECLAIMER · LARGE", seats: [
    { name: "PILOT", role: "Pilot", chans: ["Intercom","Role","Wing","Fleet"] },
    { name: "CO-PILOT", role: "Co-pilot", chans: ["Intercom","Role"] },
    { name: "ENGINEER-A", role: "Engineer", chans: ["Intercom","Role"] },
    { name: "ENGINEER-B", role: "Engineer", chans: ["Intercom","Role"] },
    { name: "SALVAGE-OP", role: "Salvager", chans: ["Intercom","Role"] },
    { name: "TURRET-A", role: "Gunner", chans: ["Intercom","Role"] },
    { name: "TURRET-B", role: "Gunner", chans: ["Intercom","Role"] },
    { name: "CARGO-MASTER", role: "Logistician", chans: ["Intercom","Fleet"] },
  ]},
];

Object.assign(window, { ScreenPlayers, ScreenServer, ScreenAdmin, SHIP_ROSTER, ROLES });
