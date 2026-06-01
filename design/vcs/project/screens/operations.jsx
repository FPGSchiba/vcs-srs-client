/* global React, Icon, StatusPill, Panel, Tac, Field, Segmented, Toggle */
/* ============================================================
   Operations — list view + calendar view + operation detail
   ============================================================ */
const { useState: useOpsState, useMemo: useOpsMemo } = React;

function ScreenOperations({ app, setApp }) {
  const [mode, setMode] = useOpsState("list"); // list | calendar
  const [calMode, setCalMode] = useOpsState("week"); // month | week | day
  const [filterState, setFilterState] = useOpsState("all");
  const [filterCat, setFilterCat] = useOpsState("all");
  const [filterFC, setFilterFC] = useOpsState("all");
  const [onlyMine, setOnlyMine] = useOpsState(false);
  const [query, setQuery] = useOpsState("");

  const filtered = useOpsMemo(() => OPS_DATA.filter(o =>
    (filterState === "all" || o.state === filterState) &&
    (filterCat === "all" || o.category === filterCat) &&
    (filterFC === "all" || o.fc === filterFC) &&
    (!onlyMine || o.participating) &&
    (!query || o.name.toLowerCase().includes(query.toLowerCase()) || o.fc.toLowerCase().includes(query.toLowerCase()))
  ), [filterState, filterCat, filterFC, onlyMine, query]);

  return (
    <div style={{ height: "100%", display: "flex", flexDirection: "column", minHeight: 0 }}>
      {/* Header / filters */}
      <div style={{ padding: "10px 14px", background: "var(--bg-0)", borderBottom: "1px solid var(--bd-1)", display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap" }}>
        <Segmented value={mode} onChange={setMode} options={[
          { value: "list", label: "LIST", icon: "list" },
          { value: "calendar", label: "CALENDAR", icon: "grid" },
        ]}/>
        <span className="sep-v" style={{ height: 20 }}/>
        <input className="input" placeholder="Search by op name or FC…" value={query} onChange={e => setQuery(e.target.value)} style={{ width: 240 }}/>
        <select className="input" value={filterState} onChange={e => setFilterState(e.target.value)} style={{ width: 130 }}>
          <option value="all">All states</option>
          {OP_STATES.map(s => <option key={s}>{s}</option>)}
        </select>
        <select className="input" value={filterCat} onChange={e => setFilterCat(e.target.value)} style={{ width: 140 }}>
          <option value="all">All categories</option>
          {OP_CATS.map(c => <option key={c}>{c}</option>)}
        </select>
        <select className="input" value={filterFC} onChange={e => setFilterFC(e.target.value)} style={{ width: 140 }}>
          <option value="all">Any FC</option>
          {[...new Set(OPS_DATA.map(o => o.fc))].map(fc => <option key={fc}>{fc}</option>)}
        </select>
        <div className="row acenter gap-3" style={{ marginLeft: 6 }}>
          <Toggle on={onlyMine} onChange={setOnlyMine}/>
          <span className="cap" style={{ color: onlyMine ? "var(--ac-primary)" : "var(--tx-3)" }}>MY PARTICIPATION ONLY</span>
        </div>
        <span className="flex"/>
        <span className="cap-dim mono">{filtered.length} OF {OPS_DATA.length} OPERATIONS</span>
      </div>

      <div style={{ flex: 1, overflow: "auto", minHeight: 0 }}>
        {mode === "list" ? (
          <OpsList ops={filtered} setApp={setApp}/>
        ) : (
          <OpsCalendar mode={calMode} setMode={setCalMode} ops={OPS_DATA} setApp={setApp}/>
        )}
      </div>
    </div>
  );
}

/* ----- LIST view ----- */
function OpsList({ ops, setApp }) {
  return (
    <table className="tbl">
      <thead>
        <tr>
          <th>Operation</th>
          <th>Category</th>
          <th>FC</th>
          <th>Start</th>
          <th>Duration</th>
          <th>Seats</th>
          <th>State</th>
          <th style={{ width: 220 }}>Actions</th>
        </tr>
      </thead>
      <tbody>
        {ops.map(o => (
          <tr key={o.id} style={{ cursor: "pointer" }} onClick={() => setApp({ view: "operationDetail", operationId: o.id })}>
            <td>
              <span style={{ color: "var(--tx-0)", fontWeight: 500 }}>{o.name}</span>
              {o.participating && <span className="cap" style={{ color: "var(--ac-primary)", marginLeft: 6 }}>● JOINED</span>}
            </td>
            <td>
              <span className="pill" style={{ color: CAT_COLOR[o.category], borderColor: CAT_COLOR[o.category] + "55" }}>
                <span className="dot" style={{ background: CAT_COLOR[o.category] }}/>
                {o.category.toUpperCase()}
              </span>
            </td>
            <td>{o.fc}</td>
            <td className="mono">{o.start}</td>
            <td className="mono">{o.duration}</td>
            <td className="mono">
              {o.filled}/{o.total}
              {o.open > 0 && <span className="cap" style={{ color: "var(--ac-warn)", marginLeft: 6 }}>{o.open} OPEN</span>}
            </td>
            <td><StatePill state={o.state}/></td>
            <td onClick={e => e.stopPropagation()}>
              <div className="row gap-2">
                <button className="btn btn-sm" onClick={() => setApp({ view: "operationDetail", operationId: o.id })}>VIEW</button>
                {o.open > 0 && !o.participating && o.state !== "Completed" && o.state !== "Cancelled" && (
                  <button className="btn btn-sm btn-primary">JOIN</button>
                )}
                {o.participating && o.state !== "Completed" && (
                  <button className="btn btn-sm btn-danger">LEAVE</button>
                )}
              </div>
            </td>
          </tr>
        ))}
        {ops.length === 0 && (
          <tr><td colSpan={8} style={{ padding: 32, textAlign: "center", color: "var(--tx-3)" }}>No operations match the current filters.</td></tr>
        )}
      </tbody>
    </table>
  );
}

function StatePill({ state }) {
  const c = {
    "Scheduled": "afk",
    "Briefing": "discipline",
    "Active": "nominal",
    "Stand-down": "afk",
    "Completed": "afk",
    "Cancelled": "critical",
  }[state] || "afk";
  return <span className={`pill pill-${c}`}><span className="dot"/>{state.toUpperCase()}</span>;
}

/* ----- CALENDAR view ----- */
function OpsCalendar({ mode, setMode, ops, setApp }) {
  return (
    <div style={{ padding: 14, height: "100%", display: "grid", gridTemplateColumns: "1fr 280px", gap: 12, minHeight: 0 }}>
      <div className="col gap-3" style={{ minHeight: 0 }}>
        <div className="row acenter gap-4">
          <button className="btn btn-sm"><Icon name="chevron" size={11} style={{ transform: "rotate(180deg)" }}/></button>
          <span style={{ fontSize: 16, color: "var(--tx-0)" }}>May 28 — June 03 · 2026</span>
          <button className="btn btn-sm"><Icon name="chevron" size={11}/></button>
          <button className="btn btn-sm">TODAY</button>
          <span className="flex"/>
          <Segmented value={mode} onChange={setMode} options={[
            { value: "month", label: "MONTH" },
            { value: "week", label: "WEEK" },
            { value: "day", label: "DAY" },
          ]}/>
        </div>
        {mode === "week" && <WeekCalendar ops={ops} setApp={setApp}/>}
        {mode === "month" && <MonthCalendar ops={ops} setApp={setApp}/>}
        {mode === "day" && <DayCalendar ops={ops} setApp={setApp}/>}
      </div>
      <Panel title="◆ TODAY · MAY 28">
        <div className="col gap-2">
          {ops.filter(o => o.day === "Thu").map(o => (
            <div key={o.id} style={{ padding: 8, border: "1px solid var(--bd-2)", borderLeft: `3px solid ${CAT_COLOR[o.category]}`, borderRadius: 3, cursor: "pointer", background: "var(--bg-2)" }} onClick={() => setApp({ view: "operationDetail", operationId: o.id })}>
              <div className="row between acenter">
                <span style={{ fontSize: 12, color: "var(--tx-0)" }}>{o.name}</span>
                <StatePill state={o.state}/>
              </div>
              <div className="mono" style={{ fontSize: 10, color: "var(--tx-3)", marginTop: 2 }}>{o.start} · {o.duration} · FC {o.fc}</div>
            </div>
          ))}
          {!ops.some(o => o.day === "Thu") && (
            <div className="cap-dim" style={{ padding: 12, textAlign: "center" }}>No operations scheduled today.</div>
          )}
        </div>
      </Panel>
    </div>
  );
}

function WeekCalendar({ ops, setApp }) {
  const days = ["Mon","Tue","Wed","Thu","Fri","Sat","Sun"];
  const dates = ["May 25","May 26","May 27","May 28","May 29","May 30","May 31"];
  const hours = Array.from({ length: 12 }, (_, i) => 12 + i); // 12:00 — 23:00
  const today = "Thu";
  const nowH = 21.2; // marker at ~21:12
  return (
    <div className="tac" style={{ flex: 1, display: "flex", flexDirection: "column", minHeight: 0, overflow: "hidden" }}>
      <div style={{ display: "grid", gridTemplateColumns: "56px repeat(7, 1fr)", borderBottom: "1px solid var(--bd-2)", background: "var(--bg-2)" }}>
        <div/>
        {days.map((d,i) => (
          <div key={d} style={{ padding: "8px 6px", borderLeft: "1px solid var(--bd-1)", background: d === today ? "rgba(96,165,250,0.06)" : "transparent" }}>
            <div className="cap" style={{ color: d === today ? "var(--ac-primary)" : "var(--tx-2)" }}>{d.toUpperCase()}</div>
            <div className="mono" style={{ fontSize: 10, color: d === today ? "var(--ac-primary)" : "var(--tx-3)", marginTop: 2 }}>{dates[i]}</div>
          </div>
        ))}
      </div>
      <div style={{ flex: 1, overflow: "auto", position: "relative", minHeight: 0 }}>
        <div style={{ display: "grid", gridTemplateColumns: "56px repeat(7, 1fr)", position: "relative" }}>
          {/* hour labels */}
          <div className="col">
            {hours.map(h => (
              <div key={h} style={{ height: 40, padding: "2px 6px", borderTop: "1px solid var(--bd-1)" }}>
                <span className="mono" style={{ fontSize: 9, color: "var(--tx-3)" }}>{String(h).padStart(2,"0")}:00</span>
              </div>
            ))}
          </div>
          {/* day columns */}
          {days.map(d => (
            <div key={d} style={{ position: "relative", borderLeft: "1px solid var(--bd-1)", background: d === today ? "rgba(96,165,250,0.03)" : "transparent" }}>
              {hours.map(h => <div key={h} style={{ height: 40, borderTop: "1px solid var(--bd-1)" }}/>)}
              {/* today's current-time marker */}
              {d === today && (
                <div style={{ position: "absolute", left: 0, right: 0, top: (nowH - 12) * 40, height: 0, borderTop: "1px solid var(--ac-warn)", boxShadow: "0 0 4px var(--ac-warn)", zIndex: 3 }}>
                  <span style={{ position: "absolute", left: -2, top: -3, width: 6, height: 6, borderRadius: "50%", background: "var(--ac-warn)" }}/>
                </div>
              )}
              {/* ops for this day */}
              {ops.filter(o => o.day === d).map(o => {
                const top = (o.startH - 12) * 40;
                const height = Math.max(28, o.durH * 40 - 2);
                return (
                  <div
                    key={o.id}
                    onClick={() => setApp({ view: "operationDetail", operationId: o.id })}
                    style={{
                      position: "absolute", left: 4, right: 4, top, height,
                      background: CAT_COLOR[o.category] + "22",
                      borderLeft: `3px solid ${CAT_COLOR[o.category]}`,
                      border: `1px solid ${CAT_COLOR[o.category]}55`,
                      borderRadius: 3,
                      padding: "4px 6px",
                      cursor: "pointer",
                      overflow: "hidden",
                      zIndex: 2,
                    }}
                  >
                    <div style={{ fontSize: 11, color: "var(--tx-0)", fontWeight: 500, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{o.name}</div>
                    <div className="mono" style={{ fontSize: 9, color: "var(--tx-3)", marginTop: 2 }}>{o.start} · {o.duration}</div>
                    {height > 60 && <div className="cap-dim" style={{ marginTop: 2, fontSize: 9 }}>{o.fc}</div>}
                  </div>
                );
              })}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

function MonthCalendar({ ops, setApp }) {
  // Render May 2026, 6 rows × 7 cols
  const startDow = 4; // May 1, 2026 = Friday (0=Mon)
  const days = 31;
  const today = 28;
  const cells = [];
  for (let i = 0; i < 42; i++) {
    const d = i - startDow + 1;
    cells.push(d >= 1 && d <= days ? d : null);
  }
  const opsByDate = {};
  ops.forEach(o => { (opsByDate[o.date] = opsByDate[o.date] || []).push(o); });
  return (
    <div className="tac" style={{ flex: 1, display: "flex", flexDirection: "column", overflow: "hidden", minHeight: 0 }}>
      <div style={{ display: "grid", gridTemplateColumns: "repeat(7, 1fr)", background: "var(--bg-2)", borderBottom: "1px solid var(--bd-2)" }}>
        {["MON","TUE","WED","THU","FRI","SAT","SUN"].map(d => (
          <div key={d} className="cap" style={{ padding: "6px 8px", borderLeft: "1px solid var(--bd-1)", color: "var(--tx-2)" }}>{d}</div>
        ))}
      </div>
      <div style={{ flex: 1, display: "grid", gridTemplateColumns: "repeat(7, 1fr)", gridAutoRows: "minmax(0, 1fr)", minHeight: 0 }}>
        {cells.map((d, i) => (
          <div key={i} style={{
            borderLeft: "1px solid var(--bd-1)", borderTop: "1px solid var(--bd-1)",
            background: d === today ? "rgba(96,165,250,0.06)" : "transparent",
            padding: 4, overflow: "hidden", minHeight: 0,
            position: "relative",
          }}>
            {d ? (
              <>
                <div className="mono" style={{ fontSize: 10, color: d === today ? "var(--ac-primary)" : "var(--tx-3)", marginBottom: 3 }}>{String(d).padStart(2,"0")}</div>
                <div className="col gap-2">
                  {(opsByDate[d] || []).slice(0,3).map(o => (
                    <div key={o.id} onClick={() => setApp({ view: "operationDetail", operationId: o.id })} style={{
                      padding: "2px 4px", background: CAT_COLOR[o.category] + "22",
                      borderLeft: `2px solid ${CAT_COLOR[o.category]}`, borderRadius: 2, cursor: "pointer",
                      fontSize: 10, color: "var(--tx-0)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap",
                    }}>
                      {o.start.split(" ")[1] || o.start} {o.name.replace(/^OP\s*/i, "")}
                    </div>
                  ))}
                  {opsByDate[d] && opsByDate[d].length > 3 && (
                    <div className="cap-dim" style={{ fontSize: 9 }}>+{opsByDate[d].length - 3} more</div>
                  )}
                </div>
              </>
            ) : null}
          </div>
        ))}
      </div>
    </div>
  );
}

function DayCalendar({ ops, setApp }) {
  const dayOps = ops.filter(o => o.day === "Thu");
  return (
    <Tac title="◆ THURSDAY · MAY 28 · 2026" accessory={<span className="cap-dim">3 operations</span>} style={{ flex: 1 }}>
      <div className="col gap-3">
        {dayOps.map(o => (
          <div key={o.id} style={{ padding: 12, border: "1px solid var(--bd-2)", borderLeft: `3px solid ${CAT_COLOR[o.category]}`, borderRadius: 3, background: "var(--bg-2)", cursor: "pointer" }} onClick={() => setApp({ view: "operationDetail", operationId: o.id })}>
            <div className="row between acenter">
              <div className="col">
                <span style={{ fontSize: 14, color: "var(--tx-0)" }}>{o.name}</span>
                <span className="mono" style={{ fontSize: 11, color: "var(--tx-3)", marginTop: 2 }}>{o.start} · {o.duration} · FC {o.fc}</span>
              </div>
              <div className="row gap-3">
                <span className="pill" style={{ color: CAT_COLOR[o.category], borderColor: CAT_COLOR[o.category] + "55" }}>
                  <span className="dot" style={{ background: CAT_COLOR[o.category] }}/>{o.category.toUpperCase()}
                </span>
                <StatePill state={o.state}/>
              </div>
            </div>
            <div className="row gap-3 acenter" style={{ marginTop: 10 }}>
              <span className="cap-dim mono">{o.filled}/{o.total} SEATS · {o.open} OPEN</span>
              <span className="flex"/>
              {o.open > 0 && !o.participating && <button className="btn btn-sm btn-primary">JOIN</button>}
              <button className="btn btn-sm">OPEN DETAIL</button>
            </div>
          </div>
        ))}
      </div>
    </Tac>
  );
}

/* ----- Operation Detail ----- */
function ScreenOperationDetail({ app, setApp }) {
  const op = OPS_DATA.find(o => o.id === app.operationId) || OPS_DATA[0];
  return (
    <div style={{ height: "100%", display: "flex", flexDirection: "column", minHeight: 0 }}>
      {/* Sub-header */}
      <div style={{ padding: "10px 14px", background: "var(--bg-0)", borderBottom: "1px solid var(--bd-1)", display: "flex", alignItems: "center", gap: 12 }}>
        <button className="btn btn-ghost btn-sm" onClick={() => setApp({ view: "operations" })}>
          <Icon name="chevron" size={11} style={{ transform: "rotate(180deg)" }}/> OPERATIONS
        </button>
        <span className="sep-v" style={{ height: 20 }}/>
        <span style={{ fontSize: 16, color: "var(--tx-0)" }}>{op.name}</span>
        <span className="pill" style={{ color: CAT_COLOR[op.category], borderColor: CAT_COLOR[op.category] + "55" }}>
          <span className="dot" style={{ background: CAT_COLOR[op.category] }}/>{op.category.toUpperCase()}
        </span>
        <StatePill state={op.state}/>
        <span className="cap-dim mono">{op.start} · {op.duration} · FC {op.fc}</span>
        <span className="cap-dim mono">{op.filled}/{op.total} SEATS{op.open > 0 ? ` · ${op.open} OPEN` : ""}</span>
        <span className="flex"/>
        {op.participating ? (
          <button className="btn btn-danger">LEAVE OPERATION</button>
        ) : (
          <button className="btn btn-primary"><Icon name="plus" size={11}/> JOIN OPERATION</button>
        )}
        {op.state === "Active" && (
          <button className="btn"><Icon name="fleet" size={11}/> OPEN IN FLEET MODE</button>
        )}
        <button className="btn">SET AS ACTIVE OP</button>
      </div>

      <div style={{ flex: 1, overflow: "auto", padding: 14, minHeight: 0, display: "grid", gridTemplateColumns: "1.3fr 1fr", gap: 12, gridTemplateRows: "auto auto auto" }}>
        {/* Briefing — 5-paragraph order */}
        <Panel title="◆ BRIEFING · 5-PARAGRAPH ORDER" style={{ gridColumn: "1 / 2", gridRow: "1 / 4" }} accessory={<span className="cap-dim mono">Authored by FC · {op.start.split(" ")[0]}</span>}>
          <div className="col gap-4" style={{ fontSize: 12, color: "var(--tx-1)", lineHeight: 1.6, textWrap: "pretty" }}>
            <BriefSection num="1" title="SITUATION">
              Pirate cell operating in <b>STANTON · MICROTECH · ASTEROID FIELD-04</b>. Three Gladius-class hostiles
              confirmed at MICRO-7 NORTH. Allied Vanguard Wing-4 holding station at 8.1km. Civilian Tide-Walker
              salvager has reported low fuel and is in our extraction corridor.
            </BriefSection>
            <BriefSection num="2" title="MISSION">
              Sweep ASTEROID FIELD-04, neutralize confirmed hostiles, extract intel from any disabled vessel.
              No collateral damage to allied or civilian assets. Op concludes with debrief at FOB ALPHA.
            </BriefSection>
            <BriefSection num="3" title="EXECUTION">
              <ul style={{ paddingLeft: 16, margin: "4px 0", lineHeight: 1.6 }}>
                <li><b>Phase 1 (T+0):</b> Rendezvous at MICRO-7. Power 30/70/0 pre-QT.</li>
                <li><b>Phase 2 (T+10):</b> Recon sweep by Phoenix-Eye. Mark all signals on Contact List.</li>
                <li><b>Phase 3 (T+30):</b> Engage. Shinobi wing leads; Persephone holds dorsal overwatch.</li>
                <li><b>Phase 4 (T+90):</b> Salvage by Reclaimer-7 if disabled vessel present.</li>
                <li><b>Phase 5 (T+120):</b> Extract and RTB.</li>
              </ul>
            </BriefSection>
            <BriefSection num="4" title="SERVICE & SUPPORT">
              Reclaimer-7 carries spare parts and an extra med kit. Phoenix-Eye is recovery asset on the
              extract line. Bingo fuel call by any unit triggers an immediate regroup.
            </BriefSection>
            <BriefSection num="5" title="COMMAND & SIGNAL">
              FC: <b>FPGSchiba</b>. Wing leads on <b>122.750 (encrypted)</b>. Engineers monitor <b>144.000</b>
              for damage coordination. Emergency Guard <b>121.500</b> always-monitored. Brevity codes per
              standard Vanguard SOP.
            </BriefSection>
          </div>
        </Panel>

        {/* Frequency plan */}
        <Panel title="◆ FREQUENCY PLAN" accessory={<span className="cap-dim mono">5 CHANNELS</span>}>
          <table className="tbl">
            <thead><tr><th>Label</th><th>Freq</th><th>Enc</th><th>Team</th></tr></thead>
            <tbody>
              {OP_FREQS.map(f => (
                <tr key={f.label}>
                  <td><span style={{ color: "var(--tx-0)" }}>{f.label}</span></td>
                  <td className="mono" style={{ color: "var(--ac-lcd)" }}>{f.freq}</td>
                  <td>{f.enc ? <Icon name="lock" size={11} style={{ color: "var(--ac-warn)" }}/> : <span style={{ color: "var(--tx-4)" }}>—</span>}</td>
                  <td><span className="cap" style={{ color: f.team === "All" ? "var(--tx-2)" : "var(--ac-primary)" }}>{f.team}</span></td>
                </tr>
              ))}
            </tbody>
          </table>
        </Panel>

        {/* Objectives */}
        <Panel title="◆ OBJECTIVES" accessory={<span className="cap-dim mono">3 / 7 COMPLETE</span>}>
          <div className="col gap-2">
            {OP_OBJS.map((o,i) => (
              <div key={i} className="row acenter gap-3" style={{ padding: 5, borderBottom: i < OP_OBJS.length - 1 ? "1px solid var(--bd-1)" : "none" }}>
                <ObjGlyph state={o.state}/>
                <span style={{ fontSize: 12, color: o.state === "complete" ? "var(--tx-3)" : "var(--tx-0)", textDecoration: o.state === "complete" ? "line-through" : "none", flex: 1 }}>{o.title}</span>
                {o.progress && <span className="mono" style={{ fontSize: 10, color: "var(--ac-primary)" }}>{o.progress}</span>}
              </div>
            ))}
          </div>
        </Panel>

        {/* Ship roster */}
        <Panel title="◆ SHIP ROSTER · SEATS" style={{ gridColumn: "1 / 3" }}>
          <table className="tbl">
            <thead><tr><th>Ship</th><th>Class</th><th>Captain</th><th>Seats</th><th>Sign-ups</th></tr></thead>
            <tbody>
              {OP_ROSTER.map(s => (
                <tr key={s.name}>
                  <td><span style={{ color: "var(--tx-0)" }}>{s.name}</span></td>
                  <td><span className="cap-dim mono" style={{ fontSize: 10 }}>{s.class}</span></td>
                  <td>{s.captain}</td>
                  <td className="mono">{s.seats.filter(x => x.player).length}/{s.seats.length}</td>
                  <td>
                    <div className="row gap-2" style={{ flexWrap: "wrap" }}>
                      {s.seats.map((seat, j) => (
                        <span key={j} className="pill" style={{ padding: "1px 8px", fontSize: 9, background: seat.player ? "var(--bg-2)" : "rgba(245,165,36,0.06)", borderColor: seat.player ? "var(--bd-2)" : "var(--ac-warn-dim)", color: seat.player ? "var(--tx-1)" : "var(--ac-warn)" }}>
                          <span className="dot" style={{ background: seat.player ? "var(--ac-ok)" : "var(--ac-warn)" }}/>
                          {seat.name} · {seat.player || "OPEN"}
                          {!seat.player && seat.eligible && <button className="btn btn-sm" style={{ marginLeft: 6, height: 16, padding: "0 6px", fontSize: 9 }}>JOIN</button>}
                        </span>
                      ))}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </Panel>
      </div>
    </div>
  );
}

function BriefSection({ num, title, children }) {
  return (
    <div>
      <div className="row acenter gap-3" style={{ marginBottom: 4 }}>
        <span className="mono" style={{ fontSize: 11, color: "var(--ac-primary)", letterSpacing: "0.18em" }}>§{num}</span>
        <span className="cap" style={{ color: "var(--ac-primary)" }}>{title}</span>
      </div>
      <div>{children}</div>
    </div>
  );
}

function ObjGlyph({ state }) {
  if (state === "complete") return <div style={{ width: 12, height: 12, borderRadius: 2, background: "var(--ac-ok)", display: "grid", placeItems: "center", flex: "0 0 12px" }}><span style={{ color: "#000", fontSize: 8 }}>✓</span></div>;
  if (state === "in progress") return <div style={{ width: 12, height: 12, borderRadius: 2, border: "1px solid var(--ac-primary)", display: "grid", placeItems: "center", flex: "0 0 12px" }}><span style={{ width: 4, height: 4, background: "var(--ac-primary)", boxShadow: "0 0 4px var(--ac-primary)" }}/></div>;
  return <div style={{ width: 12, height: 12, borderRadius: 2, border: "1px solid var(--bd-2)", flex: "0 0 12px" }}/>;
}

/* ----- Data ----- */
const OP_STATES = ["Scheduled","Briefing","Active","Stand-down","Completed","Cancelled"];
const OP_CATS = ["Combat","Mining","Medical","Salvage","Exploration","Logistics"];
const CAT_COLOR = {
  Combat: "#ef4f4f",
  Mining: "#f5b94b",
  Medical: "#5eead4",
  Salvage: "#f5a524",
  Exploration: "#60a5fa",
  Logistics: "#a78bfa",
};

const OPS_DATA = [
  { id: "op-starwalk",  name: "OP STARWALK",   category: "Combat",      state: "Active",     fc: "FPGSchiba",  start: "Thu 20:30", date: 28, day: "Thu", startH: 20.5, durH: 2.5, duration: "2h 30m", filled: 12, total: 16, open: 4, participating: true },
  { id: "op-tidepool",  name: "OP TIDEPOOL",   category: "Salvage",     state: "Briefing",   fc: "FPGElphi",   start: "Thu 22:00", date: 28, day: "Thu", startH: 22, durH: 1.75, duration: "1h 45m", filled: 4,  total: 8,  open: 4, participating: false },
  { id: "op-cinderline",name: "OP CINDERLINE", category: "Logistics",   state: "Scheduled",  fc: "JohnMckeel", start: "Fri 01:30", date: 29, day: "Fri", startH: 13.5, durH: 4.5, duration: "4h 30m", filled: 9,  total: 24, open: 15, participating: false },
  { id: "op-foxfire",   name: "OP FOXFIRE",    category: "Combat",      state: "Stand-down", fc: "Deathtype",  start: "Sat 19:00", date: 30, day: "Sat", startH: 19, durH: 3, duration: "3h 00m", filled: 0,  total: 6,  open: 0, participating: false },
  { id: "op-lodestar",  name: "OP LODESTAR",   category: "Exploration", state: "Scheduled",  fc: "Dabble",     start: "Sun 17:00", date: 31, day: "Sun", startH: 17, durH: 2, duration: "2h 00m", filled: 3,  total: 6,  open: 3, participating: true },
  { id: "op-redcross",  name: "OP REDCROSS",   category: "Medical",     state: "Completed",  fc: "Mokushiroku", start: "Wed 18:00", date: 27, day: "Wed", startH: 18, durH: 1.5, duration: "1h 30m", filled: 4,  total: 4,  open: 0, participating: true },
  { id: "op-quarry",    name: "OP QUARRY",     category: "Mining",      state: "Cancelled",  fc: "Spaceharvest", start: "Tue 21:00", date: 26, day: "Tue", startH: 21, durH: 2, duration: "2h 00m", filled: 2,  total: 12, open: 10, participating: false },
];

const OP_FREQS = [
  { label: "Fleet Common",  freq: "118.500", enc: false, team: "All" },
  { label: "Discovery Wing", freq: "122.750", enc: true, team: "Discovery" },
  { label: "Shinobi Wing",   freq: "131.500", enc: true, team: "Shinobi" },
  { label: "Engineers",     freq: "144.000", enc: true, team: "All" },
  { label: "Emergency Guard", freq: "121.500", enc: false, team: "All" },
];

const OP_OBJS = [
  { title: "Rendezvous at MICRO-7", state: "complete" },
  { title: "Establish defensive perimeter", state: "complete" },
  { title: "Sweep ASTEROID FIELD-04", state: "in progress", progress: "3/5 sectors" },
  { title: "Extract intel from disabled vessel", state: "in progress" },
  { title: "Respond to distress · Tide-Walker", state: "in progress" },
  { title: "Return to base", state: "pending" },
  { title: "Debrief at FOB ALPHA", state: "pending" },
];

const OP_ROSTER = [
  { name: "ARK-04 Persephone", class: "CARRACK · EXPLR", captain: "FPGSchiba", seats: [
    { name: "PILOT", player: "FPGSchiba" },
    { name: "CO-PILOT", player: "Vanderwolf" },
    { name: "TURRET-D", player: null, eligible: true },
    { name: "ENGINEER-A", player: "Mokushiroku" },
    { name: "MEDIC", player: "Dabble" },
  ]},
  { name: "Phoenix-Eye", class: "TERRAPIN · RECON", captain: "Dabble", seats: [
    { name: "PILOT", player: "Dabble" },
    { name: "SCANNER", player: null, eligible: false },
  ]},
  { name: "Black Lance", class: "HORNET · FIGHTER", captain: "Deathtype", seats: [
    { name: "PILOT", player: "Deathtype" },
  ]},
  { name: "Reclaimer-7", class: "RECLAIMER · SLVG", captain: "FPGElphi", seats: [
    { name: "PILOT", player: "FPGElphi" },
    { name: "CO-PILOT", player: "JohnMckeel" },
    { name: "ENGINEER-A", player: null, eligible: true },
    { name: "SALVAGE-OP", player: "Spaceharvest" },
    { name: "TURRET-A", player: "I_Die_a_lot" },
    { name: "CARGO", player: "Vanderwolf" },
  ]},
];

Object.assign(window, { ScreenOperations, ScreenOperationDetail, OPS_DATA, CAT_COLOR });
