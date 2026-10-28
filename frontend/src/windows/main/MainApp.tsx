import { useEffect, useState, type ReactElement } from "react";
import { api } from "../../shared/api/client";
import { on, EV } from "../../shared/api/events";
import { useSession } from "../../shared/store/session";
import type { Conn } from "../../shared/store/session";
import { useWindows } from "../../shared/store/windows";
import { useSettingsSync } from "../../shared/store/useSettingsSync";
import { useConnectionSync } from "../../shared/store/useConnectionSync";
import { useNotificationsSync } from "../../shared/store/useNotificationsSync";
import { useClientStateSync } from "../../shared/store/useClientStateSync";
import { useSessionEndedNotice } from "../../shared/store/useSessionEndedNotice";
import { hydrateSnapshot } from "../../shared/store/hydrate";
import { TopBar } from "../../shared/components/TopBar";
import { NavRail } from "../../shared/components/NavRail";
import { StatusBar } from "../../shared/components/StatusBar";
import { ConnBanner } from "../../shared/components/ConnBanner";
import { ToastHost } from "../../shared/components/ToastHost";
import { Welcome } from "./screens/Welcome";
import { Home } from "./screens/Home";
import { Players } from "./screens/Players";
import { History } from "./screens/History";
import { Profiles } from "./screens/Profiles";
import { Placeholder } from "./screens/Placeholder";
import { SettingsScreen } from "./screens/settings/SettingsScreen";

/** The console screen for a nav key; anything unmapped is the placeholder.
 * `nav` switches the active screen, for screens that link to another one. */
const SCREENS = (nav: (view: string) => void): Record<string, () => ReactElement> => ({
  home: () => <Home />,
  players: () => <Players />,
  history: () => <History />,
  profiles: () => <Profiles />,
  settings: () => <SettingsScreen onNavigate={nav} />,
});

/**
 * MainApp is the main-window shell. It gates on the session `phase`: before the
 * control link is `connected` it renders the guest login (Welcome); once
 * connected it renders the console (TopBar + NavRail + screen + StatusBar) with
 * lightweight view-state navigation held in local state (no router this phase).
 *
 * Client state (clients, radios, self, the selection) is owned by
 * `useClientStateSync`, which hydrates it, keeps it live and clears it on
 * logout. This shell keeps only the session phase/connection state and the
 * open-window list. Logout disconnects the control link; the resulting
 * `auth:session_changed` -> "logged_out" event returns the UI to Welcome.
 */
export function MainApp() {
  const phase = useSession((s) => s.phase);
  const [view, setView] = useState("home");

  // Keeps the shared settings/keybinds store live for this window's whole
  // lifetime, not just while the Settings screen happens to be open.
  useSettingsSync();
  // Same discipline for connection health: the status bar and the banner
  // both read it, and both are part of the window shell.
  useConnectionSync();
  // And again for notifications: the badge, the bell and ToastHost all read
  // this store, and all are part of the window shell.
  useNotificationsSync();

  // Clients, radios, self and the selection: hydrated, kept live, and
  // cleared on logout by the shared hook (FE01, FE02).
  useClientStateSync();
  useSessionEndedNotice();

  useEffect(() => {
    // FE08: a window:state push during the hydrate is newer, so the late
    // hydrate answer is dropped.
    const windowsH = hydrateSnapshot(
      () => api.getOpenWindows(),
      (ids) => useWindows.getState().setOpen(ids),
      (err) => console.error("getOpenWindows failed", err),
    );

    const offs = [
      on<string[]>(EV.windowState, (ids) => {
        windowsH.pushed();
        useWindows.getState().setOpen(ids);
      }),
      on<Conn>(EV.controlConnection, (c) => {
        useSession.getState().setConn(c);
        if (c === "connected") useSession.getState().setPhase("connected");
      }),
      on<string>(EV.authSession, (s) => {
        if (s === "logged_out") useSession.getState().setPhase("welcome");
      }),
    ];
    return () => {
      offs.forEach((off) => off());
      windowsH.dispose();
    };
  }, []);

  if (phase !== "connected") return <Welcome />;

  const screen = (SCREENS(setView)[view] ?? (() => <Placeholder />))();

  const handleLogout = () => {
    void api.disconnect();
  };

  return (
    <div className="app bg-flat">
      <TopBar view={view} />
      <ConnBanner />
      <div className="app-body">
        <NavRail activeKey={view} onSelect={setView} onLogout={handleLogout} />
        <main className="main">{screen}</main>
      </div>
      <StatusBar onNavigate={setView} />
      <ToastHost />
    </div>
  );
}
