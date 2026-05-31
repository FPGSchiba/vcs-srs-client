/* global React, Icon, StatusPill */
/* ============================================================
   App shell — top bar, nav rail, status bar
   ============================================================ */
const { useState: useShellState, useEffect: useShellEffect, useRef: useShellRef } = React;

const NAV_ITEMS = [
  { key: "home", label: "Home", icon: "grid" },
  { key: "operations", label: "Operations", icon: "fleet" },
  { key: "players", label: "Player List", icon: "users" },
  { key: "history", label: "Transmission Log", icon: "history" },
  { key: "profiles", label: "Radio Profiles", icon: "layout" },
  { key: "server", label: "Server Details", icon: "server" },
  { key: "admin", label: "Administration", icon: "admin" },
  { key: "settings", label: "Settings", icon: "settings" },
  { key: "support", label: "Support", icon: "help" },
];

const POPOUT_LAUNCHERS = [
  { key: "comms",         label: "Comms",  icon: "comms" },
  { key: "fleet",         label: "Fleet",  icon: "fleet" },
  { key: "ship",          label: "Ship",   icon: "ship" },
  { key: "messages",      label: "Msgs",   icon: "chat" },
  { key: "notifications", label: "Notif",  icon: "bell" },
];

/* ----------- Top bar ----------- */
function TopBar({ view, app, setApp, popoutState, togglePopout, onDragMove, onDragStart, onDragEnd, onLogout, clientActive }) {
  const view_titles = {
    home: "Home",
    operations: "Operations",
    players: "Player List",
    server: "Server Details",
    admin: "Administration",
    profiles: "Radio Profiles",
    settings: "Settings",
    support: "Support",
    history: "Transmission Log",
    serverNetwork: "Server Network",
    operationDetail: "Operation Detail",
  };
  const [menuOpen, setMenuOpen] = useShellState(false);
  const menuRef = useShellRef(null);

  useShellEffect(() => {
    if (!menuOpen) return;
    const onClick = e => { if (!menuRef.current || !menuRef.current.contains(e.target)) setMenuOpen(false); };
    window.addEventListener("mousedown", onClick);
    return () => window.removeEventListener("mousedown", onClick);
  }, [menuOpen]);

  const startDrag = e => {
    if (!onDragStart) return;
    if (e.target.closest("button, select, input, .launcher-btn, .user-menu-trigger, .assignment-card")) return;
    e.preventDefault();
    const startX = e.clientX, startY = e.clientY;
    const stage = document.querySelector(".desktop");
    const scale = stage ? parseFloat(stage.dataset.scale || 1) : 1;
    onDragStart();
    const move = ev => onDragMove({ dx: (ev.clientX - startX) / scale, dy: (ev.clientY - startY) / scale });
    const up = () => {
      onDragEnd && onDragEnd();
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  };

  return (
    <div className="topbar" onPointerDown={startDrag}>
      <div className="brand">
        <div className="brand-mark">
          <svg width="24" height="24" viewBox="0 0 24 24" fill="none">
            <circle cx="12" cy="12" r="9" stroke="var(--ac-primary)" strokeWidth="1" opacity="0.5"/>
            <circle cx="12" cy="12" r="5" stroke="var(--ac-primary)" strokeWidth="1"/>
            <path d="M7 14 L12 8 L17 14" stroke="var(--ac-primary)" strokeWidth="1.5" fill="none" strokeLinecap="square"/>
            <circle cx="12" cy="12" r="1.5" fill="var(--ac-primary)"/>
          </svg>
        </div>
        <div className="col" style={{ lineHeight: 1.1 }}>
          <span className="brand-name">VCS</span>
          <span className="brand-sub">Vanguard · v3.2.1</span>
        </div>
      </div>

      <div className="topbar-crumbs">
        <span className="crumb-active">{view_titles[view] || view}</span>
        {app.profile && (
          <>
            <span className="crumb-sep">/</span>
            <span className="cap" style={{ color: "var(--tx-3)" }}>{app.user.org}</span>
            <span className="crumb-sep">·</span>
            <span className="cap" style={{ color: "var(--tx-2)" }}>{app.profile.name}{app.profile.dirty && <span style={{ color: "var(--ac-warn)" }}> *</span>}</span>
          </>
        )}
      </div>

      <div className="topbar-right">
        {/* Panel launcher strip */}
        <div className="launcher">
          {POPOUT_LAUNCHERS.map(l => {
            const p = popoutState[l.key] || { open: false, docked: false, monitor: 1 };
            const loc = !p.open ? null
              : p.docked ? "DOCKED"
              : p.monitor === 2 ? "MON·2"
              : "FLOAT";
            const isFloating = p.open && !p.docked;
            const badge = l.key === "messages" ? 3
              : l.key === "notifications" ? app.unreadNotifications
              : 0;
            return (
              <span
                key={l.key}
                className={`launcher-btn ${p.open ? "open" : ""} ${isFloating ? "floating" : ""}`}
                onClick={() => togglePopout(l.key)}
                title={`${l.label} · ${p.open ? `Open · ${loc}` : "Closed — click to open"}`}
              >
                <Icon name={l.icon} size={11}/>
                {l.label}
                <span className="dot"/>
                {loc && <span className="loc">{loc}</span>}
                {badge > 0 && <span className="badge">{badge}</span>}
              </span>
            );
          })}
        </div>

        {/* User menu */}
        <div className="user-menu-anchor" ref={menuRef}>
          <div className={`user-menu-trigger ${menuOpen ? "open" : ""}`} onClick={() => setMenuOpen(o => !o)}>
            <div className="avatar">{app.user.callsign.slice(0,2).toUpperCase()}</div>
            <div className="col" style={{ lineHeight: 1.15 }}>
              <span className="um-name">{app.user.callsign}</span>
              <span className="um-meta">{app.user.org.split(" · ")[1] || app.user.org}</span>
            </div>
            <Icon name="chevronD" size={10} style={{ color: "var(--tx-3)" }}/>
          </div>
          {menuOpen && (
            <div className="user-menu">
              <div className="user-menu-section">Account</div>
              <div className="user-menu-item">
                <Icon name="users" size={12}/>
                <div className="col flex" style={{ lineHeight: 1.2 }}>
                  <span>{app.user.callsign}</span>
                  <span className="mono" style={{ fontSize: 9, color: "var(--tx-3)", letterSpacing: "0.10em" }}>FFID {app.user.ffid}</span>
                </div>
                <StatusPill kind={app.user.status} mini/>
              </div>
              <div className="user-menu-item" onClick={() => { setApp({ view: "settings" }); setMenuOpen(false); }}>
                <Icon name="settings" size={12}/> Settings…
              </div>
              <div className="user-menu-item" onClick={() => { setApp({ view: "profiles" }); setMenuOpen(false); }}>
                <Icon name="layout" size={12}/> Radio Profiles…
              </div>
              <div className="user-menu-sep"/>
              <div className="user-menu-section">Status presence</div>
              {["available","combat","discipline","afk"].map(s => (
                <div key={s} className="user-menu-item" onClick={() => { setApp({ user: { ...app.user, status: s } }); setMenuOpen(false); }}>
                  <StatusPill kind={s}/>
                  {app.user.status === s && <Icon name="info" size={11} style={{ color: "var(--ac-primary)", marginLeft: "auto" }}/>}
                </div>
              ))}
              <div className="user-menu-sep"/>
              <div className="user-menu-item danger" onClick={() => { setMenuOpen(false); onLogout && onLogout(); }}>
                <Icon name="unlock" size={12}/> Logout · Disconnect
              </div>
            </div>
          )}
        </div>

        <div className="win-ctrl">
          <button title="Minimize"><Icon name="minimize" size={12} /></button>
          <button className="close" title="Close · Sign out" onClick={onLogout}><Icon name="close" size={12} /></button>
        </div>
      </div>
    </div>
  );
}

/* ----------- Nav rail ----------- */
function NavRail({ view, setView, app, setApp, onLogout }) {
  const [collapsed, setCollapsed] = useShellState(false);
  return (
    <nav className={`nav ${collapsed ? "collapsed" : ""}`}>
      <div className="nav-header">
        <span className="cap">Console</span>
        <button className="nav-toggle" onClick={() => setCollapsed(c => !c)} title={collapsed ? "Expand" : "Collapse"}>
          <Icon name={collapsed ? "chevron" : "chevronD"} size={12} />
        </button>
      </div>
      <div style={{ flex: 1, overflow: "auto" }}>
        <div className="nav-list" style={{ padding: "8px 6px" }}>
          {NAV_ITEMS.map(it => (
            <div
              key={it.key}
              className={`nav-item ${view === it.key || (it.key === "operations" && view === "operationDetail") ? "active" : ""}`}
              onClick={() => setView(it.key)}
              title={collapsed ? it.label : undefined}
            >
              <span className="ic"><Icon name={it.icon} size={16} /></span>
              <span className="label">{it.label}</span>
              {it.badge && <span className="badge">{it.badge}</span>}
            </div>
          ))}
        </div>
      </div>
      <div style={{ borderTop: "1px solid var(--bd-1)", padding: "6px" }}>
        <div className="nav-item" onClick={onLogout} title={collapsed ? "Logout" : undefined} style={{ color: "var(--ac-alert)" }}>
          <span className="ic"><Icon name="unlock" size={16}/></span>
          <span className="label">Logout</span>
        </div>
      </div>
      <div className="nav-footer">
        <div className="user-chip">
          <div className="avatar">{app.user.callsign.slice(0,2).toUpperCase()}</div>
          <div className="user-meta">
            <div className="user-name">{app.user.callsign}</div>
            <div className="user-ffid">FFID {app.user.ffid}</div>
          </div>
        </div>
        {!collapsed && <StatusPill kind={app.user.status} />}
      </div>
    </nav>
  );
}

/* ----------- Status bar ----------- */
function StatusBar({ app, setApp, togglePopout }) {
  const isDistributed = app.topology === "distributed";
  const ctrl = app.servers.control;
  const voice = app.servers.voice;
  return (
    <div className="statusbar">
      <span className="sb-item">v3.2.1-stable</span>
      <span className="sb-divider"></span>

      {isDistributed ? (
        <div className="dual-pill" onClick={() => setApp({ view: "serverNetwork" })} title="Open Server Network">
          <span className="seg ctrl">
            <span className={`d ${ctrl.state === "ok" ? "ok" : ctrl.state === "warn" ? "warn" : "alert"}`}/>
            <span className="lbl">CTRL</span>
            <span className="val">{ctrl.name}</span>
            <span className="ping">{ctrl.ping}ms</span>
          </span>
          <span className="seg voice">
            <span className={`d ${voice.state === "ok" ? "ok" : voice.state === "warn" ? "warn" : "alert"}`}/>
            <span className="lbl">VOICE</span>
            <span className="val">{voice.name}</span>
            <span className="ping">{voice.ping}ms</span>
          </span>
        </div>
      ) : (
        <span className="sb-item" onClick={() => setApp({ view: "serverNetwork" })} style={{ cursor: "pointer" }}>
          <span style={{ width: 5, height: 5, borderRadius: "50%", background: "var(--ac-ok)", display: "inline-block" }}/>
          vanguard-prime · {app.server.latency}ms
        </span>
      )}

      <span className="sb-divider"></span>
      <span className="sb-item">{app.server.region}</span>
      <span className="sb-spacer"></span>

      <span className="sb-btn" onClick={() => setApp({ view: "serverNetwork" })}>
        <Icon name="server" size={11} /> NETWORK
      </span>
      <span className="sb-btn" onClick={() => setApp({ view: "support" })}>
        <Icon name="help" size={11} /> HELP
      </span>
      <span
        className={`sb-btn ${app.unreadNotifications > 0 ? "has-unread" : ""}`}
        onClick={() => togglePopout && togglePopout("notifications")}
      >
        <Icon name="bell" size={11} />
        ALERTS
        {app.unreadNotifications > 0 && <span className="sb-bell-count">{app.unreadNotifications}</span>}
      </span>
    </div>
  );
}

/* ----------- Connection banner ----------- */
function ConnBanner({ state, onAction }) {
  if (state === "fully-connected") return null;
  const conf = {
    "control-only": {
      kind: "warn",
      title: "VOICE DEGRADED",
      msg: "Connection to voice server lost — control is healthy. You cannot transmit or hear traffic until reconnected.",
      action: "RECONNECT VOICE",
    },
    "voice-only": {
      kind: "warn",
      title: "CONTROL DEGRADED",
      msg: "Lost link to control server — voice continues but state is frozen. Profile changes won't persist.",
      action: "RECONNECT CONTROL",
    },
    "disconnected": {
      kind: "alert",
      title: "DISCONNECTED",
      msg: "All servers unreachable. Audio is muted. Verify network and retry.",
      action: "FULL RECONNECT",
    },
  }[state];
  if (!conf) return null;
  return (
    <div className={`conn-banner ${conf.kind}`}>
      <span className="blink"/>
      <span style={{ fontWeight: 600, letterSpacing: "0.18em" }}>{conf.title}</span>
      <span style={{ color: "var(--tx-2)", letterSpacing: 0, textTransform: "none", fontFamily: "var(--ff-sans)" }}>{conf.msg}</span>
      <span style={{ flex: 1 }}/>
      <button className="btn btn-sm" onClick={onAction}><Icon name="refresh" size={10}/> {conf.action}</button>
    </div>
  );
}

/* ----------- Minimal pre-login title bar ----------- */
function PreLoginTopBar({ onDragMove, onDragStart, onDragEnd, onClose }) {
  const startDrag = e => {
    if (e.target.closest("button")) return;
    e.preventDefault();
    const startX = e.clientX, startY = e.clientY;
    const stage = document.querySelector(".desktop");
    const scale = stage ? parseFloat(stage.dataset.scale || 1) : 1;
    onDragStart && onDragStart();
    const move = ev => onDragMove({ dx: (ev.clientX - startX) / scale, dy: (ev.clientY - startY) / scale });
    const up = () => {
      onDragEnd && onDragEnd();
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  };
  return (
    <div className="topbar" onPointerDown={startDrag} style={{ gridTemplateColumns: "auto 1fr auto" }}>
      <div className="brand">
        <div className="brand-mark">
          <svg width="24" height="24" viewBox="0 0 24 24" fill="none">
            <circle cx="12" cy="12" r="9" stroke="var(--ac-primary)" strokeWidth="1" opacity="0.5"/>
            <circle cx="12" cy="12" r="5" stroke="var(--ac-primary)" strokeWidth="1"/>
            <path d="M7 14 L12 8 L17 14" stroke="var(--ac-primary)" strokeWidth="1.5" fill="none" strokeLinecap="square"/>
            <circle cx="12" cy="12" r="1.5" fill="var(--ac-primary)"/>
          </svg>
        </div>
        <div className="col" style={{ lineHeight: 1.1 }}>
          <span className="brand-name">VCS</span>
          <span className="brand-sub">Vanguard · v3.2.1</span>
        </div>
      </div>
      <div className="topbar-crumbs">
        <span className="crumb-active">Sign In</span>
        <span className="crumb-sep">·</span>
        <span className="cap" style={{ color: "var(--tx-3)" }}>DRAG TITLE BAR TO MOVE WINDOW</span>
      </div>
      <div className="win-ctrl">
        <button title="Minimize"><Icon name="minimize" size={12}/></button>
        <button className="close" title="Close"><Icon name="close" size={12}/></button>
      </div>
    </div>
  );
}

Object.assign(window, { TopBar, NavRail, StatusBar, NAV_ITEMS, ConnBanner, POPOUT_LAUNCHERS, PreLoginTopBar });
