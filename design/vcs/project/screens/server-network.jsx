/* global React, Icon, StatusPill, Toggle, Panel, Tac, Field, Segmented */
/* ============================================================
   Server Network panel — Standalone + Distributed topology
   ============================================================ */
const { useState: useNetState } = React;

function ScreenServerNetwork({ app, setApp }) {
  const [topology, setTopology] = useNetState("distributed"); // "standalone" | "distributed"
  const [autoSwitch, setAutoSwitch] = useNetState(true);
  const [pinned, setPinned] = useNetState(null);
  const [activeVoice, setActiveVoice] = useNetState("voice-eu-02");

  return (
    <div style={{ height: "100%", padding: 14, display: "flex", flexDirection: "column", gap: 12, minHeight: 0 }}>
      {/* Header */}
      <div className="row acenter gap-5" style={{ padding: "10px 12px", background: "var(--bg-0)", border: "1px solid var(--bd-1)", borderRadius: 4 }}>
        <Icon name="server" size={16} style={{ color: "var(--ac-primary)" }}/>
        <span className="cap" style={{ color: "var(--ac-primary)" }}>SERVER NETWORK</span>
        <span className="cap-dim mono">VANGUARD OPS GROUP · EU REGION</span>
        <span className="flex"/>
        <span className="cap">TOPOLOGY (DEMO)</span>
        <Segmented value={topology} onChange={setTopology} options={[
          { value: "standalone", label: "STANDALONE" },
          { value: "distributed", label: "DISTRIBUTED" },
        ]}/>
      </div>

      {topology === "standalone" ? (
        <Panel title="◆ STANDALONE SERVER" accessory={<span className="cap-dim">Single server handles control + voice</span>}>
          <table className="tbl">
            <thead><tr><th>Server</th><th>Region</th><th>Ping</th><th>Version</th><th>Uptime</th><th>Clients</th><th>State</th></tr></thead>
            <tbody>
              <tr>
                <td>
                  <div className="row acenter gap-3">
                    <span style={{ width: 6, height: 6, borderRadius: "50%", background: "var(--ac-ok)", boxShadow: "0 0 4px var(--ac-ok)" }}/>
                    <span style={{ color: "var(--tx-0)", fontWeight: 500 }}>vanguard-prime</span>
                  </div>
                </td>
                <td>EU-WEST</td>
                <td className="mono">28ms</td>
                <td className="mono">v3.2.1</td>
                <td className="mono">3d 14h</td>
                <td className="mono">18</td>
                <td><StatusPill kind="nominal" label="ACTIVE"/></td>
              </tr>
            </tbody>
          </table>
        </Panel>
      ) : (
        <>
          {/* Control server */}
          <Panel title="◆ CONTROL SERVER" accessory={<span className="cap-dim">State · sync · auth</span>}>
            <table className="tbl">
              <thead><tr><th>Server</th><th>Region</th><th>Ping</th><th>Version</th><th>Uptime</th><th>Clients</th><th>State</th><th></th></tr></thead>
              <tbody>
                <tr>
                  <td>
                    <div className="row acenter gap-3">
                      <span style={{ width: 6, height: 6, borderRadius: "50%", background: "var(--ac-ok)", boxShadow: "0 0 4px var(--ac-ok)" }}/>
                      <span style={{ color: "var(--tx-0)", fontWeight: 500 }}>vanguard-ctrl</span>
                    </div>
                  </td>
                  <td>EU-WEST</td>
                  <td className="mono">18ms</td>
                  <td className="mono">v3.2.1</td>
                  <td className="mono">12d 06h</td>
                  <td className="mono">18</td>
                  <td><StatusPill kind="nominal" label="ACTIVE"/></td>
                  <td><button className="btn btn-sm" disabled>FAILOVER N/A</button></td>
                </tr>
              </tbody>
            </table>
          </Panel>

          {/* Voice servers */}
          <Panel title="◆ VOICE SERVERS" style={{ flex: 1, display: "flex", flexDirection: "column", minHeight: 0 }} accessory={
            <div className="row gap-4 acenter">
              {pinned && <span className="cap" style={{ color: "var(--ac-warn)" }}>● PINNED: {pinned}</span>}
              <div className="row acenter gap-3">
                <span className="cap">AUTO-SWITCH (LOWEST PING)</span>
                <Toggle on={autoSwitch} onChange={v => { setAutoSwitch(v); if (v) setPinned(null); }}/>
              </div>
            </div>
          }>
            <table className="tbl">
              <thead><tr><th>Voice Server</th><th>Region</th><th>Ping</th><th>Load</th><th>Version</th><th>Clients</th><th>State</th><th></th></tr></thead>
              <tbody>
                {VOICES.map(v => {
                  const active = activeVoice === v.name;
                  return (
                    <tr key={v.name} style={{ background: active ? "rgba(96,165,250,0.06)" : undefined }}>
                      <td>
                        <div className="row acenter gap-3">
                          {active && <span style={{ width: 6, height: 6, borderRadius: "50%", background: "var(--ac-primary)", boxShadow: "0 0 4px var(--ac-primary)" }}/>}
                          {!active && <span style={{ width: 6, height: 6, borderRadius: "50%", background: v.state === "active" ? "var(--ac-ok)" : v.state === "standby" ? "var(--tx-3)" : v.state === "degraded" ? "var(--ac-warn)" : "var(--ac-alert)" }}/>}
                          <span style={{ color: active ? "var(--ac-primary)" : "var(--tx-0)", fontWeight: 500 }}>{v.name}</span>
                          {active && <span className="cap" style={{ color: "var(--ac-primary)" }}>● ACTIVE</span>}
                          {pinned === v.name && <span className="cap" style={{ color: "var(--ac-warn)" }}><Icon name="pin" size={9}/> PINNED</span>}
                        </div>
                      </td>
                      <td>{v.region}</td>
                      <td className="mono" style={{ color: v.ping < 50 ? "var(--ac-ok)" : v.ping < 100 ? "var(--ac-warn)" : "var(--ac-alert)" }}>
                        {v.state === "offline" ? "—" : `${v.ping}ms`}
                      </td>
                      <td>
                        <div className="row acenter gap-3">
                          <div style={{ width: 60, height: 4, background: "var(--bg-1)", border: "1px solid var(--bd-1)", borderRadius: 1, overflow: "hidden" }}>
                            <div style={{ width: `${v.load}%`, height: "100%", background: v.load > 80 ? "var(--ac-alert)" : v.load > 50 ? "var(--ac-warn)" : "var(--ac-ok)" }}/>
                          </div>
                          <span className="mono" style={{ fontSize: 10, color: "var(--tx-3)" }}>{v.load}%</span>
                        </div>
                      </td>
                      <td className="mono">{v.version}</td>
                      <td className="mono">{v.clients}</td>
                      <td>
                        <StatusPill kind={
                          v.state === "active" ? "nominal" :
                          v.state === "standby" ? "afk" :
                          v.state === "degraded" ? "degraded" :
                          "offline"
                        } label={v.state.toUpperCase()}/>
                      </td>
                      <td>
                        <div className="row gap-2">
                          {!active && v.state !== "offline" && (
                            <button className="btn btn-sm" onClick={() => { setActiveVoice(v.name); setAutoSwitch(false); setPinned(v.name); }}>
                              <Icon name="refresh" size={10}/> SWITCH
                            </button>
                          )}
                          {pinned === v.name && (
                            <button className="btn btn-sm btn-ghost" onClick={() => setPinned(null)}>UNPIN</button>
                          )}
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </Panel>

          <div className="row gap-3">
            <div style={{ flex: 1, padding: 10, border: "1px solid var(--bd-2)", borderRadius: 3, background: "var(--bg-2)", fontFamily: "var(--ff-mono)", fontSize: 11, color: "var(--tx-2)" }}>
              <span className="cap" style={{ color: "var(--ac-primary)" }}>BEHAVIOR</span>
              <div style={{ marginTop: 6, color: "var(--tx-2)", textWrap: "pretty", fontFamily: "var(--ff-sans)" }}>
                Switching voice servers does not affect control state — your radios stay configured. Voice will silence for ~120ms during the handover.
                {pinned ? <> Auto-switch is disabled while a server is pinned.</> : <> Auto-switch picks the lowest-ping healthy voice server.</>}
              </div>
            </div>
            <div style={{ flex: 1, padding: 10, border: "1px solid var(--bd-2)", borderRadius: 3, background: "var(--bg-2)" }}>
              <span className="cap" style={{ color: "var(--ac-primary)" }}>FAILOVER DRY-RUN</span>
              <div className="row gap-3" style={{ marginTop: 8 }}>
                <button className="btn btn-sm">SIMULATE CTRL LOSS</button>
                <button className="btn btn-sm">SIMULATE VOICE LOSS</button>
                <button className="btn btn-sm btn-danger">SIMULATE TOTAL OUTAGE</button>
              </div>
            </div>
          </div>
        </>
      )}
    </div>
  );
}

const VOICES = [
  { name: "voice-eu-01", region: "EU-WEST",  ping: 24, load: 62, version: "v3.2.1", clients: 12, state: "active" },
  { name: "voice-eu-02", region: "EU-WEST",  ping: 28, load: 34, version: "v3.2.1", clients: 18, state: "active" },
  { name: "voice-eu-03", region: "EU-NORTH", ping: 42, load: 12, version: "v3.2.1", clients: 4,  state: "standby" },
  { name: "voice-us-east-01", region: "US-EAST", ping: 92, load: 48, version: "v3.2.1", clients: 22, state: "active" },
  { name: "voice-us-west-01", region: "US-WEST", ping: 144, load: 30, version: "v3.2.0", clients: 9, state: "degraded" },
  { name: "voice-ap-01", region: "AP-SE",     ping: 0, load: 0, version: "v3.2.1", clients: 0, state: "offline" },
];

Object.assign(window, { ScreenServerNetwork });
