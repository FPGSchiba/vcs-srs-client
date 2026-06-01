/* global React, ReactDOM,
   TopBar, NavRail, StatusBar, ConnBanner, NAV_ITEMS, POPOUT_LAUNCHERS,
   ScreenComms, OverlayWindow, makeRadio,
   WelcomeScreen,
   ScreenShip, ScreenFleet, ScreenPlayers, ScreenServer, ScreenAdmin,
   ScreenSettings, ScreenSupport,
   ScreenProfiles, ScreenMessages, ScreenHistory, ScreenNotifications,
   ScreenHome, ScreenServerNetwork,
   ScreenOperations, ScreenOperationDetail,
   PopOut, POPOUT_DEFAULTS,
   TweaksPanel, useTweaks, TweakSection, TweakRadio, TweakSlider, TweakSelect, TweakToggle,
   Icon
*/
const { useState: useAppState, useEffect: useAppEffect, useRef: useAppRef } = React;

/* ----- toast stack ----- */
function ToastStack({ toasts, dismiss }) {
  return (
    <div className="toast-stack">
      {toasts.map(t => (
        <div key={t.id} className={`toast ${t.kind || ""}`}>
          <div className="row between acenter">
            <span style={{ fontSize: 12, color: "var(--tx-0)" }}>{t.title}</span>
            <button className="btn btn-ghost btn-icon btn-sm" onClick={() => dismiss(t.id)}><Icon name="x" size={10}/></button>
          </div>
          {t.body && <div style={{ fontSize: 11, color: "var(--tx-2)", marginTop: 4, textWrap: "pretty" }}>{t.body}</div>}
        </div>
      ))}
    </div>
  );
}

function buildInitialRadios() {
  return [
    { id: "r1", index: 1, name: "Fleet Common", freq: 118.500, enc: false, encChan: 1, channel: 1, volume: 80, balance: 0, muted: false, pttKey: "F1", selectKey: "Ctrl+1", rxActive: false, lastTalker: "FPGElphi", tuned: 7, unread: 0 },
    { id: "r2", index: 2, name: "Discovery Wing", freq: 122.750, enc: false, encChan: 2, channel: 1, volume: 70, balance: 0, muted: false, pttKey: "F2", selectKey: "Ctrl+2", rxActive: false, lastTalker: "Dabble", tuned: 4, unread: 2 },
    { id: "r3", index: 3, name: "Engineers Role", freq: 144.000, enc: true, encChan: 4, channel: 2, volume: 65, balance: -10, muted: false, pttKey: "F3", selectKey: "Ctrl+3", rxActive: true, lastTalker: "FPGElphi", tuned: 9, unread: 0, role: true },
    { id: "r4", index: 4, name: "Persephone Intercom", freq: 31.250, enc: true, encChan: 7, channel: 1, volume: 90, balance: 15, muted: false, pttKey: "F4", selectKey: "Ctrl+4", rxActive: false, lastTalker: "Vanderwolf", tuned: 5, unread: 1 },
    { id: "r5", index: 5, name: "Gunners Net", freq: 122.750, enc: true, encChan: 3, channel: 1, volume: 60, balance: 0, muted: false, pttKey: "F5", selectKey: "Ctrl+5", rxActive: false, lastTalker: "Deathtype", tuned: 4, unread: 0 },
    { id: "r6", index: 6, name: "Medics Net", freq: 139.500, enc: false, encChan: 5, channel: 1, volume: 50, balance: 0, muted: false, pttKey: "F6", selectKey: "Ctrl+6", rxActive: false, lastTalker: "Mokushiroku", tuned: 2, unread: 0 },
  ];
}

function buildInitialShipComps() {
  return [
    { id: "c1", name: "Main Reactor", category: "power", health: 92, state: "nominal" },
    { id: "c2", name: "Aux Reactor", category: "power", health: 100, state: "nominal" },
    { id: "c3", name: "Port Engine", category: "engines", health: 41, state: "degraded", notes: "Took hit during contact at MICRO-7." },
    { id: "c4", name: "Starboard Engine", category: "engines", health: 88, state: "nominal" },
    { id: "c5", name: "Forward Shield", category: "shields", position: "F", health: 94, state: "nominal" },
    { id: "c6", name: "Aft Shield", category: "shields", position: "B", health: 41, state: "degraded" },
    { id: "c7", name: "Port Shield", category: "shields", position: "L", health: 78, state: "nominal" },
    { id: "c8", name: "Starboard Shield", category: "shields", position: "R", health: 62, state: "degraded" },
    { id: "c9", name: "QT Drive", category: "quantum", health: 18, state: "critical", notes: "Spool aborted at 60%." },
    { id: "c10", name: "Life Support", category: "life-support", health: 100, state: "nominal" },
    { id: "c11", name: "Forward Weapon S1", category: "weapons", health: 100, state: "nominal" },
    { id: "c12", name: "Starboard Turret S2", category: "weapons", health: 0, state: "offline", notes: "Severed during last engagement." },
    { id: "c13", name: "Long-range Sensors", category: "sensors", health: 86, state: "nominal" },
    { id: "c14", name: "Hydrogen Reserves", category: "fuel", health: 72, state: "nominal" },
    { id: "c15", name: "Quantum Reserves", category: "fuel", health: 48, state: "degraded" },
    { id: "c16", name: "Cargo Bay A", category: "cargo", health: 100, state: "disabled", notes: "Reserved for mission spoils." },
  ];
}

function buildInitialPopouts() {
  return {
    comms:         { open: true,  docked: false, monitor: 1, ...POPOUT_DEFAULTS.comms, x: 1190, y: 70, w: 540, h: 760, z: 4 },
    fleet:         { open: false, docked: false, monitor: 1, ...POPOUT_DEFAULTS.fleet, z: 3 },
    ship:          { open: false, docked: false, monitor: 1, ...POPOUT_DEFAULTS.ship, x: 60, y: 80, w: 1080, h: 720, z: 2 },
    messages:      { open: false, docked: false, monitor: 1, ...POPOUT_DEFAULTS.messages, x: 120, y: 120, w: 940, h: 680, z: 1 },
    notifications: { open: false, docked: false, monitor: 1, ...POPOUT_DEFAULTS.notifications, x: 1240, y: 140, w: 460, h: 600, z: 0 },
  };
}

function useStageScale(targetW, targetH) {
  const [scale, setScale] = useAppState(1);
  useAppEffect(() => {
    const compute = () => {
      const sx = window.innerWidth / targetW;
      const sy = window.innerHeight / targetH;
      const s = Math.min(sx, sy, 1);
      setScale(s);
    };
    compute();
    window.addEventListener("resize", compute);
    return () => window.removeEventListener("resize", compute);
  }, [targetW, targetH]);
  return scale;
}

/* ============================================================
   App
   ============================================================ */
function App() {
  const [loggedIn, setLoggedIn] = useAppState(false);
  const scale = useStageScale(1760, 1080);

  const TWEAK_DEFAULTS = /*EDITMODE-BEGIN*/{
    "background": "flat",
    "radioCount": 6,
    "connection": "fully-connected",
    "topology": "distributed"
  }/*EDITMODE-END*/;
  const [tweaks, setTweak] = useTweaks(TWEAK_DEFAULTS);

  /* Client window position (drag) */
  const [clientPos, setClientPos] = useAppState({ x: 160, y: 90 });
  const [dragging, setDragging] = useAppState(false);

  const [app, setAppRaw] = useAppState(() => ({
    view: "home",
    operationId: null,
    user: { callsign: "FPGSchiba", ffid: "VG-0271-DSC", org: "Vanguard · Discovery", status: "combat" },
    assignment: { ship: "ARK-04 Persephone", seat: "PILOT", role: "Pilot" },

    topology: "distributed",
    connection: "fully-connected",
    servers: {
      control: { name: "vanguard-ctrl", ping: 18, state: "ok" },
      voice:   { name: "voice-eu-02",   ping: 28, state: "ok" },
    },
    server: { name: "vanguard-prime.eu", latency: 28, region: "EU-WEST" },

    profile: { id: "p1", name: "Fleet Op — Stanton", dirty: false },
    profiles: [
      { id: "p1", name: "Fleet Op — Stanton", file: "fleet-op-stanton.vcs.json", modified: "2026-05-19 21:00", radioCount: 6, layout: "POWER", desc: "Default loadout for Stanton ops. Discovery wing primary.", author: "FPGSchiba" },
      { id: "p2", name: "Solo Patrol", file: "solo-patrol.vcs.json", modified: "2026-05-12 14:33", radioCount: 3, layout: "2×2", desc: "Quiet patrol with three monitored channels.", author: "FPGSchiba" },
      { id: "p3", name: "Salvage Crew (Reclaimer)", file: "salvage-reclaimer.vcs.json", modified: "2026-05-10 09:18", radioCount: 4, layout: "2×2", desc: "Engineer-heavy comms with intercom + role + fleet.", author: "FPGElphi" },
      { id: "p4", name: "Shinobi Strike", file: "shinobi-strike.vcs.json", modified: "2026-05-04 22:01", radioCount: 8, layout: "POWER", desc: "Power-user layout for combat sorties.", author: "JohnMckeel" },
      { id: "p5", name: "Compact (Streaming)", file: "compact-streaming.vcs.json", modified: "2026-04-30 18:14", radioCount: 2, layout: "STRIP", desc: "Minimal strip overlay, no chrome.", author: "Spaceharvest" },
    ],
    radios: buildInitialRadios(),
    selectedRadioId: "r1",
    layoutPreset: "default",
    globalPtt: "Space",
    shipComponents: buildInitialShipComps(),
    shipSync: { broadcast: true, lastSync: "21:15:08" },
    recentTraffic: [
      { time: "21:15:51", from: "Dabble", freq: "118.500", dur: 3.2 },
      { time: "21:15:44", from: "FPGSchiba", freq: "118.500", dur: 4.8, self: true },
      { time: "21:15:12", from: "I_Die_a_lot", freq: "122.750", dur: 2.1 },
      { time: "21:15:08", from: "Deathtype", freq: "122.750", dur: 2.4 },
      { time: "21:15:02", from: "FPGSchiba", freq: "118.500", dur: 8.1, self: true },
      { time: "21:14:28", from: "ColdSpoke", freq: "144.000", dur: 1.6 },
      { time: "21:14:21", from: "Vanderwolf", freq: "144.000", dur: 2.9 },
      { time: "21:14:01", from: "FPGElphi", freq: "144.000", dur: 7.4 },
    ],
    presets: [
      { name: "STANTON · FLEET", freq: 118.500, tag: "primary", color: "var(--ac-primary)" },
      { name: "STANTON · GUNNERS", freq: 122.750, tag: "encrypted", enc: true },
      { name: "STANTON · PILOTS", freq: 127.250, tag: "role" },
      { name: "STANTON · ENGINEERS", freq: 144.000, tag: "encrypted role", enc: true },
      { name: "STANTON · MEDICS", freq: 139.500, tag: "role" },
      { name: "PERSEPHONE · INTERCOM", freq: 31.250, tag: "intercom", enc: true },
      { name: "EMERGENCY · GUARD", freq: 121.500, tag: "fleet-wide", color: "var(--ac-alert)" },
    ],
    unreadNotifications: 4,
    toasts: [],
  }));

  const setApp = patch => setAppRaw(a => ({ ...a, ...patch }));

  /* ----- Pop-out state ----- */
  const [popouts, setPopouts] = useAppState(buildInitialPopouts);
  const [activePopout, setActivePopout] = useAppState("comms");
  const zRef = useAppRef(10);

  const togglePopout = key => {
    setPopouts(p => {
      const cur = p[key];
      if (cur.open) {
        return { ...p, [key]: { ...cur, open: false } };
      } else {
        return { ...p, [key]: { ...cur, open: true, docked: false, z: ++zRef.current } };
      }
    });
    setActivePopout(key);
  };
  const focusPopout = key => {
    setPopouts(p => ({ ...p, [key]: { ...p[key], z: ++zRef.current } }));
    setActivePopout(key);
  };
  const movePopout = (key, xy) => setPopouts(p => ({ ...p, [key]: { ...p[key], ...xy } }));
  const sizePopout = (key, wh) => setPopouts(p => ({ ...p, [key]: { ...p[key], ...wh } }));

  /* ----- Tweaks → app state ----- */
  useAppEffect(() => {
    setAppRaw(a => {
      const ctrl = { ...a.servers.control };
      const voice = { ...a.servers.voice };
      switch (tweaks.connection) {
        case "fully-connected":
          ctrl.state = "ok"; voice.state = "ok"; break;
        case "control-only":
          ctrl.state = "ok"; voice.state = "alert"; break;
        case "voice-only":
          ctrl.state = "alert"; voice.state = "ok"; break;
        case "disconnected":
          ctrl.state = "alert"; voice.state = "alert"; break;
      }
      return { ...a, connection: tweaks.connection, topology: tweaks.topology, servers: { control: ctrl, voice } };
    });
  }, [tweaks.connection, tweaks.topology]);

  useAppEffect(() => {
    const want = tweaks.radioCount;
    setAppRaw(a => {
      if (a.radios.length === want) return a;
      if (a.radios.length < want) {
        const next = [...a.radios];
        for (let i = a.radios.length; i < want; i++) next.push(makeRadio(i + 1));
        return { ...a, radios: next };
      } else {
        return { ...a, radios: a.radios.slice(0, want) };
      }
    });
  }, [tweaks.radioCount]);

  /* ----- Demo: rx flicker ----- */
  useAppEffect(() => {
    const t = setInterval(() => {
      setAppRaw(a => {
        const i = Math.floor(Math.random() * a.radios.length);
        const next = a.radios.map((r, j) => j === i ? { ...r, rxActive: !r.rxActive } : r);
        return { ...a, radios: next };
      });
    }, 4200);
    return () => clearInterval(t);
  }, []);

  /* ----- Auto-dismiss toasts ----- */
  useAppEffect(() => {
    if (!app.toasts.length) return;
    const t = setTimeout(() => setAppRaw(a => ({ ...a, toasts: a.toasts.slice(1) })), 6500);
    return () => clearTimeout(t);
  }, [app.toasts]);

  /* ----- Demo toast on login ----- */
  useAppEffect(() => {
    if (loggedIn) {
      setTimeout(() => {
        setAppRaw(a => ({
          ...a, toasts: [...a.toasts, { id: Date.now(), kind: "warn", title: "Distress beacon · Spaceharvest", body: "Origin: STANTON · MICRO-7. Tune to 118.500 to assist." }],
        }));
      }, 1800);
    }
  }, [loggedIn]);

  /* Drag handler for client window */
  const onClientDragStart = () => setDragging(true);
  const onClientDragEnd   = () => setDragging(false);
  const onClientDragMove = ({ dx, dy }) => {
    setClientPos(p => {
      // origin position at drag start: we stash on ref
      return { x: clientDragOrigin.current.x + dx, y: clientDragOrigin.current.y + dy };
    });
  };
  const clientDragOrigin = useAppRef(clientPos);
  // capture origin every time dragging starts
  useAppEffect(() => { if (dragging) clientDragOrigin.current = clientPos; }, [dragging]);

  const handleLogout = () => {
    setLoggedIn(false);
    setApp({ view: "home" });
    setPopouts(buildInitialPopouts());
    setClientPos({ x: 160, y: 90 });
  };

  const popoutsByZ = Object.entries(popouts).filter(([_, p]) => p.open).sort((a, b) => a[1].z - b[1].z);

  const renderPopoutBody = key => {
    switch (key) {
      case "comms":         return <ScreenComms app={app} setApp={setApp}/>;
      case "fleet":         return <ScreenFleet app={app} setApp={setApp}/>;
      case "ship":          return <ScreenShip app={app} setApp={setApp}/>;
      case "messages":      return <ScreenMessages app={app} setApp={setApp}/>;
      case "notifications": return <ScreenNotifications app={app} setApp={setApp} togglePopout={togglePopout}/>;
    }
  };

  return (
    <div className="viewport">
      <div className="desktop" data-scale={scale} style={{ transform: `scale(${scale})` }}>
        {/* Main client (fixed 1440x900, draggable) */}
        <div
          className={`client ${dragging || !loggedIn ? "active" : ""}`}
          style={{ left: clientPos.x, top: clientPos.y }}
        >
          {!loggedIn ? (
            <WelcomeScreen
              onLogin={() => setLoggedIn(true)}
              onDragStart={onClientDragStart}
              onDragEnd={onClientDragEnd}
              onDragMove={onClientDragMove}
            />
          ) : (
            <div className={`app bg-${tweaks.background}`}>
              <TopBar
                view={app.view}
                app={app}
                setApp={setApp}
                popoutState={popouts}
                togglePopout={togglePopout}
                onDragStart={onClientDragStart}
                onDragEnd={onClientDragEnd}
                onDragMove={onClientDragMove}
                onLogout={handleLogout}
                clientActive={dragging}
              />

              <ConnBanner state={app.connection} onAction={() => setTweak("connection", "fully-connected")}/>

              <div className="app-body">
                <NavRail view={app.view} setView={v => setApp({ view: v })} app={app} setApp={setApp} onLogout={handleLogout}/>
                <main className="main">
                  {app.view === "home" && <ScreenHome app={app} setApp={setApp} togglePopout={togglePopout} popoutState={popouts}/>}
                  {app.view === "operations" && <ScreenOperations app={app} setApp={setApp}/>}
                  {app.view === "operationDetail" && <ScreenOperationDetail app={app} setApp={setApp}/>}
                  {app.view === "players" && <ScreenPlayers app={app} setApp={setApp}/>}
                  {app.view === "server" && <ScreenServer app={app} setApp={setApp}/>}
                  {app.view === "admin" && <ScreenAdmin app={app} setApp={setApp}/>}
                  {app.view === "profiles" && <ScreenProfiles app={app} setApp={setApp}/>}
                  {app.view === "settings" && <ScreenSettings app={app} setApp={setApp}/>}
                  {app.view === "support" && <ScreenSupport app={app} setApp={setApp}/>}
                  {app.view === "history" && <ScreenHistory app={app} setApp={setApp}/>}
                  {app.view === "serverNetwork" && <ScreenServerNetwork app={app} setApp={setApp}/>}
                </main>
              </div>

              <StatusBar app={app} setApp={setApp} togglePopout={togglePopout}/>
            </div>
          )}
        </div>

        {/* Pop-out windows (only when logged in) */}
        {loggedIn && popoutsByZ.map(([key, p]) => (
          <PopOut
            key={key}
            id={key}
            title={POPOUT_DEFAULTS[key].title}
            meta={POPOUT_DEFAULTS[key].meta}
            x={p.x} y={p.y} w={p.w} h={p.h}
            minW={POPOUT_DEFAULTS[key].minW}
            minH={POPOUT_DEFAULTS[key].minH}
            active={activePopout === key}
            onFocus={() => focusPopout(key)}
            onMove={xy => movePopout(key, xy)}
            onResize={wh => sizePopout(key, wh)}
            onClose={() => togglePopout(key)}
            onMinimize={() => togglePopout(key)}
          >
            {renderPopoutBody(key)}
          </PopOut>
        ))}

        {loggedIn && <ToastStack toasts={app.toasts} dismiss={id => setApp({ toasts: app.toasts.filter(t => t.id !== id) })}/>}
      </div>

      <TweaksPanel title="Tweaks">
        <TweakSection label="Background" />
        <TweakRadio
          label="Treatment"
          value={tweaks.background}
          onChange={v => setTweak("background", v)}
          options={[
            { value: "flat", label: "Flat" },
            { value: "grid", label: "Grid" },
            { value: "scanlines", label: "Lines" },
          ]}
        />
        <TweakSection label="Radio panel" />
        <TweakSlider
          label="Radio count"
          value={tweaks.radioCount}
          onChange={v => setTweak("radioCount", v)}
          min={1} max={12} step={1}
        />
        <TweakSection label="Server topology" />
        <TweakRadio
          label="Mode"
          value={tweaks.topology}
          onChange={v => setTweak("topology", v)}
          options={[
            { value: "standalone", label: "Stand" },
            { value: "distributed", label: "Dist" },
          ]}
        />
        <TweakSection label="Connection state" />
        <TweakSelect
          label="State"
          value={tweaks.connection}
          onChange={v => setTweak("connection", v)}
          options={[
            { value: "fully-connected", label: "Fully connected" },
            { value: "control-only", label: "Control only · voice degraded" },
            { value: "voice-only", label: "Voice only · control degraded" },
            { value: "disconnected", label: "Disconnected" },
          ]}
        />
      </TweaksPanel>
    </div>
  );
}

ReactDOM.createRoot(document.getElementById("root")).render(<App/>);
