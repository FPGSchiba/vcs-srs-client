/* global React, Icon, Toggle, KeyChip, Panel, Field, Tac, StateCard, StatusPill, Segmented, VU, Knob, HealthBar, LcdFreq */
/* ============================================================
   Settings + Support
   ============================================================ */
const { useState: useSettingsState } = React;

function ScreenSettings({ app, setApp }) {
  const [section, setSection] = useSettingsState("general");
  const sections = [
    { key: "general", label: "General" },
    { key: "keybinds", label: "Keybinds" },
    { key: "audio", label: "Audio & Sounds" },
    { key: "effects", label: "Radio Effects" },
    { key: "profiles", label: "Profiles & Layouts" },
    { key: "notif", label: "Notifications" },
    { key: "misc", label: "Miscellaneous" },
    { key: "legacy", label: "Legacy" },
  ];
  return (
    <div style={{ height: "100%", display: "grid", gridTemplateColumns: "200px 1fr", minHeight: 0 }}>
      <div style={{ borderRight: "1px solid var(--bd-1)", background: "var(--bg-0)", padding: 10 }}>
        {sections.map(s => (
          <div
            key={s.key}
            onClick={() => setSection(s.key)}
            className={`nav-item ${section === s.key ? "active" : ""}`}
            style={{ gridTemplateColumns: "1fr" }}
          >
            <span className="label">{s.label}</span>
          </div>
        ))}
      </div>
      <div style={{ overflow: "auto", padding: 16, minHeight: 0 }}>
        {section === "general" && <SettingsGeneral/>}
        {section === "keybinds" && <SettingsKeybinds app={app} setApp={setApp}/>}
        {section === "audio" && <SettingsAudio/>}
        {section === "effects" && <SettingsEffects/>}
        {section === "profiles" && <SettingsProfiles app={app} setApp={setApp}/>}
        {section === "notif" && <SettingsNotif/>}
        {section === "misc" && <SettingsMisc/>}
        {section === "legacy" && <SettingsLegacy/>}
      </div>
    </div>
  );
}

function SettingRow({ label, desc, control }) {
  return (
    <div className="row between acenter" style={{ padding: "12px 0", borderBottom: "1px solid var(--bd-1)", gap: 16 }}>
      <div className="col" style={{ flex: 1, minWidth: 0 }}>
        <span style={{ fontSize: 13, color: "var(--tx-0)" }}>{label}</span>
        {desc && <span className="cap-dim" style={{ fontSize: 10, marginTop: 2, textTransform: "none", letterSpacing: "0.04em" }}>{desc}</span>}
      </div>
      {control}
    </div>
  );
}

function SettingsGeneral() {
  const [v, setV] = useSettingsState({ minStart: false, tray: true, txName: true, connSounds: true, switchPTT: false });
  return (
    <Panel title="GENERAL">
      <SettingRow label="Start minimized" desc="Launch VCS as an icon in the system tray." control={<Toggle on={v.minStart} onChange={x => setV({...v, minStart: x})} lg/>}/>
      <SettingRow label="Minimize to tray" desc="Closing the window keeps VCS running in tray." control={<Toggle on={v.tray} onChange={x => setV({...v, tray: x})} lg/>}/>
      <SettingRow label="Show transmitter name" desc="Display caller's callsign on incoming transmissions." control={<Toggle on={v.txName} onChange={x => setV({...v, txName: x})} lg/>}/>
      <SettingRow label="Play connection sounds" desc="SFX on connect / disconnect / reconnect." control={<Toggle on={v.connSounds} onChange={x => setV({...v, connSounds: x})} lg/>}/>
      <SettingRow label="Radio switch as PTT" desc="Hardware radio mode-switch acts as push-to-talk while held." control={<Toggle on={v.switchPTT} onChange={x => setV({...v, switchPTT: x})} lg/>}/>
    </Panel>
  );
}

function SettingsKeybinds({ app, setApp }) {
  const radios = app.radios;
  return (
    <div className="col gap-5">
      <Panel title="GLOBAL">
        <KbdRow label="Global PTT" desc="Transmits on the currently Selected radio" binding={app.globalPtt} onRebind={k => setApp({ globalPtt: k })}/>
        <KbdRow label="Push-to-mute" binding="V"/>
        <KbdRow label="Mute toggle" binding="M"/>
        <KbdRow label="Emergency broadcast" binding="Ctrl+E"/>
        <KbdRow label="Open compact overlay" binding="Ctrl+O"/>
      </Panel>
      <Panel title="CHANNEL HOTKEYS">
        <KbdRow label="Intercom" binding="1"/>
        <KbdRow label="Role channel" binding="2"/>
        <KbdRow label="Ship-wide" binding="3"/>
        <KbdRow label="Fleet-wide" binding="4"/>
      </Panel>
      <Panel title="PER-RADIO BINDINGS">
        <table className="tbl">
          <thead><tr><th>Radio</th><th>Frequency</th><th>PTT</th><th>Select</th><th></th></tr></thead>
          <tbody>
            {radios.map(r => (
              <tr key={r.id}>
                <td><span style={{ color: "var(--tx-0)" }}>R{String(r.index).padStart(2,"0")} · {r.name}</span></td>
                <td className="mono" style={{ color: "var(--ac-lcd)" }}>{r.freq.toFixed(3)}</td>
                <td><KeyChip binding={r.pttKey} onRebind={k => setApp({ radios: app.radios.map(x => x.id === r.id ? {...x, pttKey: k} : x) })}/></td>
                <td><KeyChip binding={r.selectKey} onRebind={k => setApp({ radios: app.radios.map(x => x.id === r.id ? {...x, selectKey: k} : x) })}/></td>
                <td>
                  <button className="btn btn-sm btn-ghost" onClick={() => setApp({ radios: app.radios.map(x => x.id === r.id ? {...x, pttKey: null, selectKey: null} : x) })}>UNBIND</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </Panel>
      <Panel title="QUICK-STATUS HOTKEYS">
        <KbdRow label="Set status: Available" binding="Alt+1" right={<StatusPill kind="available"/>}/>
        <KbdRow label="Set status: In Combat" binding="Alt+2" right={<StatusPill kind="combat"/>}/>
        <KbdRow label="Set status: Comms Discipline" binding="Alt+3" right={<StatusPill kind="discipline"/>}/>
        <KbdRow label="Set status: AFK" binding="Alt+4" right={<StatusPill kind="afk"/>}/>
      </Panel>
    </div>
  );
}

function KbdRow({ label, desc, binding, onRebind, right }) {
  return (
    <div className="row between acenter" style={{ padding: "10px 0", borderBottom: "1px solid var(--bd-1)" }}>
      <div className="col" style={{ flex: 1 }}>
        <span style={{ fontSize: 13, color: "var(--tx-0)" }}>{label}</span>
        {desc && <span className="cap-dim" style={{ fontSize: 10, marginTop: 2, textTransform: "none", letterSpacing: "0.04em" }}>{desc}</span>}
      </div>
      {right && <span style={{ marginRight: 12 }}>{right}</span>}
      <KeyChip binding={binding} onRebind={onRebind}/>
      <button className="btn btn-sm btn-ghost" style={{ marginLeft: 8 }}>UNBIND</button>
    </div>
  );
}

function SettingsAudio() {
  const [vol, setVol] = useSettingsState(75);
  const [vox, setVox] = useSettingsState(true);
  const [agc, setAgc] = useSettingsState(true);
  const [ns, setNs] = useSettingsState(true);
  return (
    <div className="col gap-5">
      <Panel title="DEVICES">
        <SettingRow label="Microphone" desc="USB Procyon Headset MK-II" control={
          <select className="input" style={{ width: 240 }}>
            <option>USB Procyon Headset MK-II</option>
            <option>System Default</option>
            <option>Vanguard XLR Bay</option>
          </select>
        }/>
        <SettingRow label="Speakers / Headphones" desc="USB Procyon Headset MK-II" control={
          <select className="input" style={{ width: 240 }}><option>USB Procyon Headset MK-II</option></select>
        }/>
        <SettingRow label="Mic passthrough" desc="Mix mic audio into your own output." control={<Toggle on={false} lg onChange={() => {}}/>}/>
      </Panel>
      <Panel title="MIC TEST">
        <div className="row acenter gap-6">
          <button className="btn btn-primary"><Icon name="mic" size={11}/> TEST MIC</button>
          <div className="col gap-2" style={{ flex: 1 }}>
            <span className="cap-dim">Live input</span>
            <VU level={0.4} segs={16}/>
          </div>
          <Knob value={vol} onChange={setVol} label="MASTER" size={48}/>
        </div>
      </Panel>
      <Panel title="PROCESSING">
        <SettingRow label="Automatic Gain Control (AGC)" control={<Toggle on={agc} onChange={setAgc} lg/>}/>
        <SettingRow label="Noise Suppression" control={<Toggle on={ns} onChange={setNs} lg/>}/>
        <SettingRow label="Voice activation (VOX)" desc="Open channel when mic level passes threshold." control={<Toggle on={vox} onChange={setVox} lg/>}/>
        {vox && (
          <>
            <SettingRow label="VOX threshold" control={<input type="range" min="0" max="100" defaultValue="35" style={{ width: 200 }}/>}/>
            <SettingRow label="VOX min length" desc="Minimum sustain time before triggering." control={<input className="input mono" defaultValue="220 ms" style={{ width: 100 }}/>}/>
            <SettingRow label="VOX noise cancel" control={<Toggle on={true} onChange={() => {}} lg/>}/>
          </>
        )}
        <SettingRow label="PTT start delay" desc="Pause before mic opens after key down." control={<input className="input mono" defaultValue="0 ms" style={{ width: 100 }}/>}/>
        <SettingRow label="PTT release delay" desc="Tail before mic closes after key up." control={<input className="input mono" defaultValue="120 ms" style={{ width: 100 }}/>}/>
        <SettingRow label="Recording" desc="Record incoming transmissions for replay (per server permission)." control={<Toggle on={false} onChange={() => {}} lg/>}/>
      </Panel>
    </div>
  );
}

function SettingsEffects() {
  const effects = [
    ["TX Start", "transmit_open.wav", true],
    ["TX End", "transmit_close.wav", true],
    ["RX Start", "receive_open.wav", true],
    ["RX End", "receive_close.wav", true],
    ["Intercom Start", "intercom_open.wav", true],
    ["Intercom End", "intercom_close.wav", true],
    ["Encryption Beep", "crypto_handshake.wav", true],
    ["Voice Effect", "comms_filter_mid.preset", true],
    ["Clipping Effect", "saturated_overdrive.preset", false],
  ];
  return (
    <Panel title="RADIO EFFECTS">
      {effects.map(([name, file, on]) => (
        <div key={name} className="row between acenter" style={{ padding: "10px 0", borderBottom: "1px solid var(--bd-1)" }}>
          <span style={{ width: 160, fontSize: 12 }}>{name}</span>
          <select className="input mono" style={{ flex: 1, marginRight: 12 }}>
            <option>{file}</option>
          </select>
          <button className="btn btn-sm" style={{ marginRight: 8 }}><Icon name="volume" size={11}/> PREVIEW</button>
          <Toggle on={on} onChange={() => {}}/>
        </div>
      ))}
    </Panel>
  );
}

function SettingsProfiles({ app, setApp }) {
  return (
    <div className="col gap-5">
      <Panel title="PROFILES & LAYOUTS">
        <SettingRow label="Profiles directory" desc="Where exported and discovered profiles live." control={
          <div className="row gap-3">
            <input className="input mono" defaultValue="C:\Users\Schiba\Documents\Vanguard\Profiles" style={{ width: 380 }}/>
            <button className="btn btn-sm"><Icon name="folder" size={11}/> BROWSE</button>
          </div>
        }/>
        <SettingRow label="Default profile on launch" control={
          <select className="input"><option>Fleet Op — Stanton</option><option>Solo Patrol</option><option>Last used</option></select>
        }/>
        <SettingRow label="Auto-save on exit" desc="Save changes to active profile when closing VCS." control={<Toggle on={true} onChange={() => {}} lg/>}/>
        <SettingRow label="Export format" control={
          <Segmented value="json" onChange={() => {}} options={[{ value: "json", label: "JSON" }, { value: "yaml", label: "YAML" }, { value: "toml", label: "TOML" }]}/>
        }/>
        <SettingRow label="Manage profiles" control={<button className="btn btn-primary" onClick={() => setApp({ view: "profiles" })}>OPEN PROFILE MANAGER</button>}/>
      </Panel>
    </div>
  );
}

function SettingsNotif() {
  const cats = [
    ["Joined your frequency", "joinFreq", true, true, false],
    ["Distress beacon", "distress", true, true, false],
    ["Fleet-wide alert", "fleet", true, true, false],
    ["Server reconnect", "reconnect", true, false, false],
    ["Profile load complete", "profile", true, false, true],
    ["Ship Mode sync error", "sync", true, true, false],
    ["Whisper received", "whisper", true, true, false],
    ["Admin action against you", "modAction", true, true, false],
  ];
  return (
    <Panel title="NOTIFICATION PREFERENCES">
      <table className="tbl">
        <thead><tr><th>Category</th><th style={{ width: 80 }}>Toast</th><th style={{ width: 80 }}>Sound</th><th style={{ width: 80 }}>Silent</th></tr></thead>
        <tbody>
          {cats.map(([name, k, t, s, si]) => (
            <tr key={k}>
              <td><span style={{ color: "var(--tx-0)" }}>{name}</span></td>
              <td><Toggle on={t} onChange={() => {}}/></td>
              <td><Toggle on={s} onChange={() => {}}/></td>
              <td><Toggle on={si} onChange={() => {}}/></td>
            </tr>
          ))}
        </tbody>
      </table>
    </Panel>
  );
}

function SettingsMisc() {
  return (
    <Panel title="MISCELLANEOUS">
      <SettingRow label="Allow telemetry" desc="Anonymous diagnostic events sent to Vanguard ops." control={<Toggle on={false} onChange={() => {}} lg/>}/>
      <SettingRow label="Crash reports" control={<Toggle on={true} onChange={() => {}} lg/>}/>
      <SettingRow label="Auto-update" control={<Toggle on={true} onChange={() => {}} lg/>}/>
      <SettingRow label="Update channel" control={<Segmented value="stable" onChange={() => {}} options={[{value:"stable",label:"STABLE"},{value:"rc",label:"RC"},{value:"nightly",label:"NIGHTLY"}]}/>}/>
    </Panel>
  );
}

function SettingsLegacy() {
  return (
    <Panel title="LEGACY">
      <SettingRow label="Legacy SRS audio engine" desc="Use the older audio engine for compatibility with v2.x servers." control={<Toggle on={false} onChange={() => {}} lg/>}/>
      <SettingRow label="Always-on legacy overlay" control={<Toggle on={false} onChange={() => {}} lg/>}/>
      <SettingRow label="Disable hardware acceleration" control={<Toggle on={false} onChange={() => {}} lg/>}/>
    </Panel>
  );
}

/* ----------------- Support ----------------- */
function ScreenSupport() {
  const [tab, setTab] = useSettingsState("faq");
  return (
    <div style={{ height: "100%", display: "flex", flexDirection: "column", minHeight: 0 }}>
      <div className="tabs" style={{ padding: "0 16px", background: "var(--bg-0)" }}>
        {[
          ["faq","FAQs & Help"],
          ["about","About & Credits"],
          ["legal","Legal"],
          ["updates","Updates"],
        ].map(([k,l]) => <span key={k} className={`tab ${tab === k ? "active" : ""}`} onClick={() => setTab(k)}>{l}</span>)}
      </div>
      <div style={{ flex: 1, overflow: "auto", padding: 16, minHeight: 0 }}>
        {tab === "faq" && <FaqTab/>}
        {tab === "about" && <AboutTab/>}
        {tab === "legal" && <LegalTab/>}
        {tab === "updates" && <UpdatesTab/>}
      </div>
    </div>
  );
}

function FaqTab() {
  const faqs = [
    ["How do I rebind PTT?", "Settings → Keybinds → Global PTT. Each radio also has its own PTT and Select slot — click the chip on the radio."],
    ["Why can I hear someone but they cannot hear me?", "Verify your transmit frequency matches theirs and that encryption keys are identical. The IRL Tx Behavior server toggle affects squelch tails."],
    ["Where are profiles saved?", "Settings → Profiles & Layouts. The default folder is your Documents · Vanguard · Profiles. Exported files can be shared in fleet channels."],
    ["Compact overlay shortcut isn't working", "Compact overlay is bound to Ctrl+O by default; if your OS captures the shortcut, rebind it in Settings → Keybinds."],
  ];
  return (
    <div>
      <input className="input" placeholder="Search the help…" style={{ width: 360, marginBottom: 12 }}/>
      <Panel title="FREQUENTLY ASKED">
        <div className="col">
          {faqs.map(([q,a], i) => (
            <details key={i} style={{ padding: "10px 0", borderBottom: "1px solid var(--bd-1)" }}>
              <summary style={{ cursor: "pointer", color: "var(--tx-0)" }}>{q}</summary>
              <div style={{ marginTop: 8, color: "var(--tx-2)", textWrap: "pretty" }}>{a}</div>
            </details>
          ))}
        </div>
      </Panel>
    </div>
  );
}

function AboutTab() {
  return (
    <Panel title="ABOUT VCS">
      <div className="row gap-6">
        <div>
          <svg width="80" height="80" viewBox="0 0 64 64" fill="none">
            <circle cx="32" cy="32" r="28" stroke="var(--ac-primary)" strokeWidth="1" opacity="0.4"/>
            <circle cx="32" cy="32" r="16" stroke="var(--ac-primary)" strokeWidth="1"/>
            <path d="M16 38 L32 18 L48 38" stroke="var(--ac-primary)" strokeWidth="2" fill="none"/>
            <circle cx="32" cy="32" r="3" fill="var(--ac-primary)"/>
          </svg>
        </div>
        <div className="col gap-3">
          <div style={{ fontSize: 22, color: "var(--tx-0)", letterSpacing: "0.08em" }}>VANGUARD COMMUNICATIONS SYSTEM</div>
          <div className="mono" style={{ color: "var(--tx-2)" }}>v3.2.1 · build 20260518 · stable</div>
          <div style={{ color: "var(--tx-2)", textWrap: "pretty", maxWidth: 480, marginTop: 8 }}>
            VCS is a fleet-grade voice comms client built on the SRS protocol. Designed by and for the Vanguard organization to coordinate large multi-ship operations.
          </div>
          <div className="cap-dim" style={{ marginTop: 16 }}>CREDITS</div>
          <div className="mono" style={{ fontSize: 11, color: "var(--tx-2)", lineHeight: 1.6 }}>
            CORE · FPGSchiba, FPGElphi, Dabble<br/>
            AUDIO · Spaceharvest, JohnMckeel<br/>
            UX · I_Die_a_lot, Deathtype<br/>
            QA · The Discovery, Shinobi and Phoenix units<br/>
          </div>
        </div>
      </div>
    </Panel>
  );
}

function LegalTab() {
  return (
    <Panel title="LEGAL">
      <div className="cap" style={{ color: "var(--ac-primary)" }}>LICENSE</div>
      <div style={{ color: "var(--tx-2)", marginTop: 6, maxWidth: 640, textWrap: "pretty" }}>
        VCS is distributed to Vanguard fleet members under a non-commercial use license. Source-available components remain under their original licenses (see <span className="mono" style={{ color: "var(--ac-primary)" }}>LICENSES.txt</span>).
      </div>
      <div className="cap" style={{ color: "var(--ac-primary)", marginTop: 16 }}>PRIVACY</div>
      <div style={{ color: "var(--tx-2)", marginTop: 6, maxWidth: 640, textWrap: "pretty" }}>
        VCS does not record voice or text unless server admin enables recording AND your status is set to a recording-permitted state. Telemetry is opt-in.
      </div>
      <div className="cap" style={{ color: "var(--ac-primary)", marginTop: 16 }}>THIRD PARTY</div>
      <div className="mono" style={{ color: "var(--tx-2)", marginTop: 6, fontSize: 11 }}>
        SimpleRadioStandalone · MIT<br/>
        Opus Audio Codec · BSD<br/>
        RNNoise · BSD<br/>
      </div>
    </Panel>
  );
}

function UpdatesTab() {
  return (
    <div className="col gap-4">
      <Panel title="UPDATES" accessory={<button className="btn btn-primary"><Icon name="refresh" size={11}/> CHECK FOR UPDATES</button>}>
        <div className="row acenter gap-5">
          <span className="cap-dim">CURRENT BUILD</span>
          <span className="mono" style={{ color: "var(--tx-0)", fontSize: 14 }}>v3.2.1-stable · 20260518</span>
          <span className="sep-v" style={{ height: 16 }}/>
          <span className="cap-dim">CHANNEL</span>
          <span className="cap" style={{ color: "var(--ac-ok)" }}>STABLE</span>
          <span className="sep-v" style={{ height: 16 }}/>
          <span className="cap" style={{ color: "var(--ac-ok)" }}>● UP TO DATE</span>
        </div>
      </Panel>
      <Panel title="RELEASE NOTES">
        <div className="col gap-4">
          {[
            { v: "v3.2.1", date: "2026-05-18", items: ["Fix VOX threshold clipping at 0%.", "Improve LCD digit rendering on hi-DPI displays.", "Compact overlay: remember last anchor position."] },
            { v: "v3.2.0", date: "2026-05-02", items: ["New: Mission Templates with auto-tune.", "New: per-radio Select keybind.", "Engine: 35% lower CPU usage on idle."] },
            { v: "v3.1.4", date: "2026-04-09", items: ["Ship Mode component sync stability.", "Hierarchy view in Player List.", "EU node added."] },
          ].map(r => (
            <div key={r.v} className="tac">
              <div className="tac-h">
                <span style={{ color: "var(--tx-0)" }}>{r.v}</span>
                <span className="cap-dim mono">{r.date}</span>
              </div>
              <div className="tac-body">
                <ul style={{ paddingLeft: 18, margin: 0, color: "var(--tx-2)", lineHeight: 1.6 }}>
                  {r.items.map((it, i) => <li key={i}>{it}</li>)}
                </ul>
              </div>
            </div>
          ))}
        </div>
      </Panel>
    </div>
  );
}

Object.assign(window, { ScreenSettings, ScreenSupport });
