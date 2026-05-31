/* global React, Icon, LcdFreq, PreLoginTopBar */
/* ============================================================
   Welcome / Login flow — rendered inside the main client window
   ============================================================ */
const { useState: useWelcomeState, useEffect: useWelcomeEffect } = React;

/* Coalitions the authenticated member belongs to, and the units within each. */
const COALITIONS = [
  { id: "vanguard",      tag: "VG",  name: "Vanguard",                role: "Full Member", desc: "Primary fleet org · Stanton theatre" },
  { id: "crusader-pact", tag: "CSP", name: "Crusader Security Pact",  role: "Allied",      desc: "Joint patrol coalition · shared guard" },
  { id: "deepcore",      tag: "DCS", name: "DeepCore Salvage Union",  role: "Reserve",     desc: "Industrial support · reclaimer ops" },
];

const UNITS = {
  vanguard: [
    { id: "discovery", code: "DSC", name: "Discovery Wing", note: "Exploration & recon", freq: "122.750", members: 14 },
    { id: "shinobi",   code: "SHN", name: "Shinobi Strike", note: "Combat sorties",      freq: "127.250", members: 9 },
    { id: "phoenix",   code: "PHX", name: "Phoenix Rescue", note: "SAR & medical",       freq: "139.500", members: 6 },
  ],
  "crusader-pact": [
    { id: "guard",  code: "GRD", name: "Guard Patrol",   note: "Lane security",     freq: "121.500", members: 11 },
    { id: "escort", code: "ESC", name: "Escort Element", note: "Convoy protection", freq: "118.500", members: 7 },
  ],
  deepcore: [
    { id: "reclaim", code: "RCL", name: "Reclaimer Crew", note: "Bulk salvage",     freq: "144.000", members: 5 },
    { id: "tow",     code: "TOW", name: "Recovery / Tow", note: "Wreck retrieval",  freq: "139.500", members: 4 },
  ],
};

function WelcomeScreen({ onLogin, onDragMove, onDragStart, onDragEnd }) {
  const [stage, setStage] = useWelcomeState("welcome");
  const [coalition, setCoalition] = useWelcomeState("vanguard");
  const [unit, setUnit] = useWelcomeState("discovery");

  const activeCoalition = COALITIONS.find(c => c.id === coalition) || COALITIONS[0];
  const units = UNITS[coalition] || [];
  const activeUnit = units.find(u => u.id === unit) || units[0];
  const ffid = `${activeCoalition.tag}-0271-${activeUnit ? activeUnit.code : ""}`;

  const pickCoalition = id => {
    setCoalition(id);
    const firstUnit = (UNITS[id] || [])[0];
    if (firstUnit) setUnit(firstUnit.id);
  };

  return (
    <div style={{ height: "100%", display: "flex", flexDirection: "column", background: "var(--bg-0)" }}>
      <PreLoginTopBar onDragMove={onDragMove} onDragStart={onDragStart} onDragEnd={onDragEnd}/>
      <div style={{ flex: 1, position: "relative", display: "grid", placeItems: "center", overflow: "hidden", minHeight: 0 }}>
        {/* faint backdrop */}
        <div style={{
          position: "absolute", inset: 0,
          background:
            "radial-gradient(ellipse at top, rgba(96,165,250,0.10), transparent 60%),"+
            "radial-gradient(ellipse at bottom, rgba(96,165,250,0.03), transparent 50%)",
          pointerEvents: "none",
        }}/>
        <div style={{
          position: "absolute", inset: 0,
          backgroundImage: "linear-gradient(rgba(96,165,250,0.04) 1px, transparent 1px), linear-gradient(90deg, rgba(96,165,250,0.04) 1px, transparent 1px)",
          backgroundSize: "40px 40px",
          maskImage: "radial-gradient(ellipse at center, black 30%, transparent 70%)",
          WebkitMaskImage: "radial-gradient(ellipse at center, black 30%, transparent 70%)",
          pointerEvents: "none",
        }}/>
        {/* radar rings */}
        <div style={{ position: "absolute", top: "50%", left: "50%", width: 720, height: 720, marginLeft: -360, marginTop: -360, borderRadius: "50%", border: "1px solid rgba(96,165,250,0.06)", pointerEvents: "none" }}/>
        <div style={{ position: "absolute", top: "50%", left: "50%", width: 520, height: 520, marginLeft: -260, marginTop: -260, borderRadius: "50%", border: "1px solid rgba(96,165,250,0.10)", pointerEvents: "none" }}/>

        <div style={{ width: stage === "select" ? 560 : 460, position: "relative", zIndex: 2, transition: "width 0.2s ease" }}>
          <div className="col center gap-3" style={{ marginBottom: 28 }}>
            <svg width="64" height="64" viewBox="0 0 64 64" fill="none">
              <circle cx="32" cy="32" r="28" stroke="var(--ac-primary)" strokeWidth="1" opacity="0.4"/>
              <circle cx="32" cy="32" r="16" stroke="var(--ac-primary)" strokeWidth="1"/>
              <path d="M16 38 L32 18 L48 38" stroke="var(--ac-primary)" strokeWidth="1.5" fill="none"/>
              <circle cx="32" cy="32" r="3" fill="var(--ac-primary)"/>
              <line x1="32" y1="4" x2="32" y2="12" stroke="var(--ac-primary)" strokeWidth="1"/>
              <line x1="32" y1="52" x2="32" y2="60" stroke="var(--ac-primary)" strokeWidth="1"/>
              <line x1="4" y1="32" x2="12" y2="32" stroke="var(--ac-primary)" strokeWidth="1"/>
              <line x1="52" y1="32" x2="60" y2="32" stroke="var(--ac-primary)" strokeWidth="1"/>
            </svg>
            <div className="cap mono" style={{ color: "var(--ac-primary)", fontSize: 11, letterSpacing: "0.32em" }}>VANGUARD COMMUNICATIONS SYSTEM</div>
            <div style={{ fontSize: 28, fontWeight: 600, letterSpacing: "0.08em", color: "var(--tx-0)" }}>V.C.S</div>
            <div className="cap" style={{ color: "var(--tx-3)" }}>FLEET COMMS · TACTICAL · ENCRYPTED</div>
          </div>

          <div className="panel" style={{ background: "rgba(10,20,32,0.7)", backdropFilter: "blur(6px)", borderColor: "var(--bd-3)" }}>
            {stage === "welcome" && (
              <div className="col gap-4" style={{ padding: 28 }}>
                <button className="btn btn-primary btn-lg" onClick={() => setStage("sso")}>
                  <Icon name="shield" size={14}/> LOGIN · ORG MEMBER (SSO)
                </button>
                <button className="btn btn-lg" onClick={() => setStage("manual")}>
                  <Icon name="server" size={14}/> JOIN AS GUEST · MANUAL SERVER
                </button>
                <div className="row center gap-3" style={{ marginTop: 8 }}>
                  <span className="cap">REGION</span>
                  <span className="cap" style={{ color: "var(--ac-primary)" }}>EU-WEST · NOMINAL</span>
                  <span style={{ color: "var(--tx-4)" }}>·</span>
                  <span className="cap">28ms</span>
                </div>
              </div>
            )}

            {stage === "sso" && (
              <div className="col gap-5" style={{ padding: 28 }}>
                <div className="row between acenter">
                  <div className="cap" style={{ color: "var(--ac-primary)" }}>VANGUARD SSO</div>
                  <button className="btn btn-ghost btn-sm" onClick={() => setStage("welcome")}>BACK</button>
                </div>
                <Field label="Email"><input className="input" defaultValue="schiba@vanguard.org"/></Field>
                <Field label="Password"><input className="input" type="password" defaultValue="••••••••••"/></Field>
                <button className="btn btn-primary btn-lg" onClick={() => setStage("select")}>
                  CONTINUE
                </button>
              </div>
            )}

            {stage === "select" && (
              <div className="col gap-5" style={{ padding: 28 }}>
                <div className="row between acenter">
                  <div className="cap" style={{ color: "var(--ac-primary)" }}>ASSIGN COALITION &amp; UNIT</div>
                  <button className="btn btn-ghost btn-sm" onClick={() => setStage("sso")}>BACK</button>
                </div>

                <div className="col gap-3">
                  <span className="field-label">Coalition</span>
                  <div className="col gap-2">
                    {COALITIONS.map(c => (
                      <SelectRow
                        key={c.id}
                        selected={c.id === coalition}
                        onClick={() => pickCoalition(c.id)}
                        tag={c.tag}
                        title={c.name}
                        note={c.desc}
                        right={c.role}
                      />
                    ))}
                  </div>
                </div>

                <div className="col gap-3">
                  <span className="field-label">Unit · {activeCoalition.name}</span>
                  <div className="col gap-2">
                    {units.map(u => (
                      <SelectRow
                        key={u.id}
                        selected={u.id === unit}
                        onClick={() => setUnit(u.id)}
                        tag={u.code}
                        title={u.name}
                        note={u.note}
                        right={`${u.members} active`}
                        sub={`${u.freq} MHz`}
                      />
                    ))}
                  </div>
                </div>

                <div className="row between acenter" style={{ padding: "10px 12px", border: "1px solid var(--bd-1)", background: "var(--bg-2)", borderRadius: 3 }}>
                  <span className="cap">ASSIGNED FFID</span>
                  <span className="mono" style={{ fontSize: 13, color: "var(--ac-primary)", letterSpacing: "0.06em" }}>{ffid}</span>
                </div>

                <button className="btn btn-primary btn-lg" onClick={() => setStage("connecting")}>
                  AUTHENTICATE
                </button>
              </div>
            )}

            {stage === "manual" && (
              <div className="col gap-5" style={{ padding: 28 }}>
                <div className="row between acenter">
                  <div className="cap" style={{ color: "var(--ac-primary)" }}>MANUAL SERVER · GUEST</div>
                  <button className="btn btn-ghost btn-sm" onClick={() => setStage("welcome")}>BACK</button>
                </div>
                <Field label="Server Address"><input className="input mono" defaultValue="srs.vanguard.org:5002"/></Field>
                <Field label="Server Password"><input className="input" type="password" defaultValue="●●●●●●●●"/></Field>
                <div className="row gap-4">
                  <Field label="Player Name" style={{ flex: 1 }}>
                    <input className="input" defaultValue="Spaceharvest"/>
                  </Field>
                  <Field label="FFID (optional)" style={{ flex: 1 }}>
                    <input className="input mono" defaultValue="—"/>
                  </Field>
                </div>
                <button className="btn btn-primary btn-lg" onClick={() => { setStage("connecting"); }}>
                  CONNECT
                </button>
              </div>
            )}

            {stage === "connecting" && (
              <div className="col gap-4" style={{ padding: 28 }}>
                <div className="row acenter gap-3" style={{ marginBottom: 4 }}>
                  <Icon name="server" size={14} style={{ color: "var(--ac-primary)" }}/>
                  <span className="cap" style={{ color: "var(--ac-primary)" }}>ESTABLISHING TACTICAL LINK</span>
                </div>
                <StagedConnect onDone={() => setStage("success")}/>
              </div>
            )}

            {stage === "success" && (
              <div className="col center gap-3" style={{ padding: 36 }}>
                <Icon name="sos" size={48} style={{ color: "var(--ac-ok)" }}/>
                <div className="cap" style={{ color: "var(--ac-ok)", letterSpacing: "0.24em" }}>LINK ESTABLISHED</div>
                <div style={{ fontSize: 18, color: "var(--tx-0)", marginTop: 4 }}>Welcome, FPGSchiba</div>
                <div className="cap" style={{ color: "var(--tx-3)" }}>{activeCoalition.name.toUpperCase()} · {activeUnit ? activeUnit.name.toUpperCase() : "—"} · DUTY ASSIGNED</div>
                <div style={{
                  marginTop: 12, padding: "12px 20px", border: "1px dashed var(--bd-2)",
                  borderRadius: 4, color: "var(--tx-2)", fontStyle: "italic", fontSize: 12, textAlign: "center",
                  fontFamily: "var(--ff-mono)",
                }}>
                  "STEADY ON THE LINE · NO COMMS, NO COHESION"
                  <div style={{ marginTop: 6, fontSize: 9, color: "var(--tx-4)", letterSpacing: "0.2em", fontStyle: "normal", textTransform: "uppercase" }}>— Vanguard Operations Doctrine</div>
                </div>
                <button className="btn btn-primary btn-lg" style={{ marginTop: 12 }} onClick={onLogin}>
                  ENTER CONSOLE
                </button>
              </div>
            )}
          </div>

          <div className="row center gap-4" style={{ marginTop: 16, color: "var(--tx-4)", fontFamily: "var(--ff-mono)", fontSize: 9, letterSpacing: "0.16em" }}>
            <span>v3.2.1-stable</span>
            <span>·</span>
            <span>SRS PROTOCOL 1.9.0</span>
            <span>·</span>
            <span>BUILD 20260518</span>
          </div>
        </div>
      </div>
    </div>
  );
}

function Field({ label, children, style }) {
  return (
    <div className="field" style={style}>
      <span className="field-label">{label}</span>
      {children}
    </div>
  );
}

function SelectRow({ selected, onClick, tag, title, note, right, sub }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="row acenter gap-3"
      style={{
        width: "100%", textAlign: "left", cursor: "pointer",
        padding: "9px 11px",
        border: `1px solid ${selected ? "var(--ac-primary)" : "var(--bd-1)"}`,
        background: selected ? "rgba(96,165,250,0.07)" : "var(--bg-1)",
        borderRadius: 3,
        transition: "border-color 0.12s ease, background 0.12s ease",
      }}
    >
      <span style={{
        width: 14, height: 14, flex: "0 0 14px", borderRadius: "50%",
        border: `1px solid ${selected ? "var(--ac-primary)" : "var(--bd-2)"}`,
        display: "grid", placeItems: "center",
      }}>
        {selected && <span style={{ width: 6, height: 6, borderRadius: "50%", background: "var(--ac-primary)" }}/>}
      </span>
      <span className="mono" style={{
        width: 34, flex: "0 0 34px", fontSize: 10, textAlign: "center",
        color: selected ? "var(--ac-primary)" : "var(--tx-3)",
        border: `1px solid ${selected ? "var(--ac-primary)" : "var(--bd-2)"}`,
        borderRadius: 2, padding: "2px 0", letterSpacing: "0.04em",
      }}>{tag}</span>
      <span className="col" style={{ flex: 1, minWidth: 0 }}>
        <span style={{ fontSize: 13, color: "var(--tx-0)", lineHeight: 1.2 }}>{title}</span>
        <span style={{ fontSize: 11, color: "var(--tx-3)", marginTop: 2 }}>{note}</span>
      </span>
      <span className="col" style={{ alignItems: "flex-end", flex: "0 0 auto" }}>
        <span className="cap" style={{ color: selected ? "var(--ac-primary)" : "var(--tx-3)" }}>{right}</span>
        {sub && <span className="mono" style={{ fontSize: 10, color: "var(--tx-4)", marginTop: 2 }}>{sub}</span>}
      </span>
    </button>
  );
}

function StagedConnect({ onDone }) {
  const stages = [
    { key: "resolve", label: "Resolving server addresses", time: 500, detail: "srs.vanguard.org → 185.84.30.12" },
    { key: "ctrl", label: "Connecting to control server", time: 700, detail: "vanguard-ctrl · EU-WEST · 18ms" },
    { key: "voice-select", label: "Selecting voice server", time: 900, detail: "Probing 6 voice candidates…" },
    { key: "voice-connect", label: "Connecting to voice server", time: 700, detail: "voice-eu-02 · EU-WEST · 28ms · LOAD 34%" },
    { key: "ready", label: "Ready", time: 500, detail: "Profile loaded · 6 radios tuned" },
  ];
  const [stepIdx, setStepIdx] = useWelcomeState(0);

  useWelcomeEffect(() => {
    if (stepIdx >= stages.length) { onDone(); return; }
    const t = setTimeout(() => setStepIdx(i => i + 1), stages[stepIdx].time);
    return () => clearTimeout(t);
  }, [stepIdx]);

  const candidates = [
    { name: "voice-eu-01", region: "EU-WEST",  ping: 24, load: 62, state: "active" },
    { name: "voice-eu-02", region: "EU-WEST",  ping: 28, load: 34, state: "active" },
    { name: "voice-eu-03", region: "EU-NORTH", ping: 42, load: 12, state: "standby" },
    { name: "voice-us-east-01", region: "US-EAST", ping: 92, load: 48, state: "active" },
    { name: "voice-us-west-01", region: "US-WEST", ping: 144, load: 30, state: "degraded" },
    { name: "voice-ap-01", region: "AP-SE", ping: 0, load: 0, state: "offline" },
  ];

  return (
    <div className="col gap-3" style={{ minWidth: 380 }}>
      {stages.map((s, i) => {
        const done = i < stepIdx;
        const active = i === stepIdx;
        return (
          <div key={s.key} className="row acenter gap-4" style={{
            padding: "8px 10px",
            border: `1px solid ${active ? "var(--ac-primary)" : "var(--bd-1)"}`,
            background: active ? "rgba(96,165,250,0.05)" : done ? "var(--bg-2)" : "var(--bg-1)",
            borderRadius: 3,
          }}>
            <div style={{ width: 18, height: 18, borderRadius: "50%", display: "grid", placeItems: "center", flex: "0 0 18px" }}>
              {done ? (
                <Icon name="info" size={12} style={{ color: "var(--ac-ok)" }}/>
              ) : active ? (
                <svg width="14" height="14" viewBox="0 0 14 14">
                  <circle cx="7" cy="7" r="5" fill="none" stroke="var(--bd-2)" strokeWidth="1"/>
                  <circle cx="7" cy="7" r="5" fill="none" stroke="var(--ac-primary)" strokeWidth="1.5" strokeDasharray="4 18" strokeLinecap="round">
                    <animateTransform attributeName="transform" type="rotate" from="0 7 7" to="360 7 7" dur="0.9s" repeatCount="indefinite"/>
                  </circle>
                </svg>
              ) : (
                <span style={{ width: 4, height: 4, borderRadius: "50%", background: "var(--bd-2)" }}/>
              )}
            </div>
            <div className="col" style={{ flex: 1 }}>
              <span style={{ fontSize: 12, color: done || active ? "var(--tx-0)" : "var(--tx-3)" }}>{i + 1}. {s.label}</span>
              {(done || active) && <span className="mono" style={{ fontSize: 10, color: "var(--tx-3)", marginTop: 2 }}>{s.detail}</span>}
            </div>
            <span className="cap" style={{ color: done ? "var(--ac-ok)" : active ? "var(--ac-primary)" : "var(--tx-4)" }}>
              {done ? "OK" : active ? "…" : "—"}
            </span>
          </div>
        );
      })}
      {stepIdx === 2 && (
        <div className="tac" style={{ marginTop: 6 }}>
          <div className="tac-h"><span className="cap">VOICE CANDIDATES</span><span className="cap-dim">probing in parallel</span></div>
          <div className="col" style={{ padding: 6, fontFamily: "var(--ff-mono)", fontSize: 10 }}>
            {candidates.map((c, i) => (
              <div key={c.name} className="row acenter gap-3" style={{ padding: "3px 4px" }}>
                <span style={{ width: 5, height: 5, borderRadius: "50%", background: c.state === "active" ? "var(--ac-ok)" : c.state === "standby" ? "var(--tx-3)" : c.state === "degraded" ? "var(--ac-warn)" : "var(--ac-alert)" }}/>
                <span style={{ color: "var(--tx-1)", width: 140 }}>{c.name}</span>
                <span style={{ color: "var(--tx-3)", width: 60 }}>{c.region}</span>
                <span style={{ color: c.state === "offline" ? "var(--ac-alert)" : c.ping < 50 ? "var(--ac-ok)" : c.ping < 100 ? "var(--ac-warn)" : "var(--ac-alert)", width: 50 }}>{c.state === "offline" ? "OFFLINE" : `${c.ping}ms`}</span>
                <span style={{ color: "var(--tx-3)" }}>load {c.load}%</span>
                <span className="flex"/>
                {c.name === "voice-eu-02" && <span className="cap" style={{ color: "var(--ac-primary)" }}>● SELECTED · lowest ping</span>}
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

Object.assign(window, { WelcomeScreen, Field });
