import { useEffect } from "react";
import { api } from "../api/client";
import { on, EV } from "../api/events";
import { useConnection, type ConnectionState } from "./connection";

/**
 * useConnectionSync hydrates the shared connection-health store and keeps it
 * live for as long as the calling window is mounted.
 *
 * It belongs to a WINDOW, not a screen — the same discipline as
 * `useSettingsSync`, and for the same reason: each window is a separate Wails
 * webview with its own JS runtime, so a store mounted inside one screen is
 * torn down the moment the user navigates away.
 *
 * The mount-time fetch exists because events are push-only: a window opened
 * AFTER a transition has missed it, and would otherwise sit on the honest
 * but frozen empty state until the next tick.
 */
export function useConnectionSync(): void {
  useEffect(() => {
    // `stale` guards the hydrate against its own lateness. The backend can
    // push a newer snapshot while this promise is still in flight, and
    // letting the resolution write unconditionally would roll the store back
    // to a state that is already wrong — with no further event guaranteed
    // to correct it until the next 5s tick.
    let stale = false;

    api
      .getConnectionState()
      .then((c) => {
        if (stale) return;
        useConnection.getState().setConn(c);
      })
      .catch((err) => {
        // Logged rather than swallowed: the store's honest empty default
        // means a rejection here leaves the surface reporting disconnected
        // with nothing measured, and no trace of why it never updates.
        console.error("getConnectionState failed; connection health is unknown", err);
      });

    const offs = [
      on<ConnectionState>(EV.connectionState, (c) => {
        // Any pushed snapshot supersedes the in-flight hydrate.
        stale = true;
        useConnection.getState().setConn(c);
      }),
    ];
    return () => {
      stale = true;
      offs.forEach((off) => off());
    };
  }, []);
}
