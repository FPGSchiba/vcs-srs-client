import { useEffect, useState } from "react";
import { api } from "../../shared/api/client";
import type { ClientInfoDTO, RadioInfoDTO } from "../../shared/api/client";
import { on, EV } from "../../shared/api/events";
import { useSession } from "../../shared/store/session";
import type { Conn } from "../../shared/store/session";
import { useClients } from "../../shared/store/clients";
import { useRadios } from "../../shared/store/radios";
import { useWindows } from "../../shared/store/windows";
import { TopBar } from "../../shared/components/TopBar";
import { NavRail } from "../../shared/components/NavRail";
import { StatusBar } from "../../shared/components/StatusBar";
import { ConnBanner } from "../../shared/components/ConnBanner";
import { Welcome } from "./screens/Welcome";
import { Home } from "./screens/Home";
import { Players } from "./screens/Players";
import { Placeholder } from "./screens/Placeholder";

interface ClientUpdatePayload {
  guid: string;
  info: ClientInfoDTO;
}
interface ClientLeftPayload {
  guid: string;
}
interface RadioUpdatePayload {
  guid: string;
  radio: RadioInfoDTO;
}

/**
 * MainApp is the main-window shell. It gates on the session `phase`: before the
 * control link is `connected` it renders the guest login (Welcome); once
 * connected it renders the console (TopBar + NavRail + screen + StatusBar) with
 * lightweight view-state navigation held in local state (no router this phase).
 *
 * On mount it hydrates the client/radio stores from a one-shot snapshot and
 * subscribes to the backend state/auth/control events, mapping each into the
 * relevant Zustand store. Logout disconnects the control link; the resulting
 * `auth:session_changed` -> "logged_out" event returns the UI to Welcome.
 */
export function MainApp() {
  const phase = useSession((s) => s.phase);
  const [view, setView] = useState("home");

  useEffect(() => {
    // Pull the full snapshot (clients, radios, self) and replace the stores.
    // Called on mount and again once the control link reports `connected`,
    // because SyncClient populates the Go store during connect without emitting
    // per-client events.
    const hydrate = () => {
      api
        .getClientState()
        .then((snap) => {
          useClients.getState().replaceAll(snap.clients ?? {});
          useRadios.getState().replaceAll(snap.radios ?? {});
          useSession.getState().setSelf(
            snap.self
              ? { callsign: snap.self.name, ffid: snap.self.unit_id, coalition: snap.self.coalition }
              : null,
          );
        })
        .catch(() => {
          /* not connected yet — ignore */
        });
    };

    hydrate();
    api
      .getOpenWindows()
      .then((ids) => useWindows.getState().setOpen(ids))
      .catch(() => {
        /* ignore */
      });

    const offs = [
      on<string[]>(EV.windowState, (ids) => useWindows.getState().setOpen(ids)),
      on<ClientUpdatePayload>(EV.clientUpdate, (d) => useClients.getState().upsert(d.guid, d.info)),
      on<ClientLeftPayload>(EV.clientLeft, (d) => useClients.getState().remove(d.guid)),
      on<RadioUpdatePayload>(EV.radioUpdate, (d) => useRadios.getState().setForGuid(d.guid, d.radio)),
      on<Conn>(EV.controlConnection, (c) => {
        useSession.getState().setConn(c);
        if (c === "connected") {
          useSession.getState().setPhase("connected");
          hydrate(); // pull the clients/self the server returned at SyncClient
        }
      }),
      on<string>(EV.authSession, (s) => {
        if (s === "logged_out") {
          useSession.getState().setPhase("welcome");
          useSession.getState().setSelf(null);
          useClients.getState().replaceAll({});
          useRadios.getState().replaceAll({});
        }
      }),
    ];
    return () => offs.forEach((off) => off());
  }, []);

  if (phase !== "connected") return <Welcome />;

  const screen =
    view === "home" ? <Home /> :
    view === "players" ? <Players /> :
    <Placeholder />;

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
      <StatusBar />
    </div>
  );
}
