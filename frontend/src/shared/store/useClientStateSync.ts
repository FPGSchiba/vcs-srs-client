import { useEffect } from "react";
import { api } from "../api/client";
import type { ClientInfoDTO, ClientStateSnapshot, RadioInfoDTO } from "../api/client";
import { on, EV } from "../api/events";
import type { HotkeyEventPayload } from "../api/events";
import { useClients } from "./clients";
import { useRadios } from "./radios";
import { useSession } from "./session";
import { hydrateDelta, hydrateSnapshot, type HydrateHandle } from "./hydrate";

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

const SELECT_RE = /^radio\.(\d+)\.select$/;

function applySnapshot(snap: ClientStateSnapshot): void {
  useClients.getState().replaceAll(snap.clients ?? {});
  useRadios.getState().replaceAll(snap.radios ?? {});
  const session = useSession.getState();
  session.setSelf(
    snap.self
      ? { callsign: snap.self.name, ffid: snap.self.unit_id, coalition: snap.self.coalition }
      : null,
  );
  session.setSelfGuid(snap.self_guid ?? "");
}

/**
 * useClientStateSync owns the client, radio and session-identity stores for
 * the calling window (Phase 6.9 CL3, FE01 + FE02). Mount it once per window
 * shell (`MainApp`, `CommsApp`): each window is its own webview with its own
 * stores, so each needs its own subscriptions.
 *
 * - Hydrates `getClientState` (clients, radios, `self`, `selfGuid`) with the
 *   DELTA rule: a `radio_update`/`client_update`/`client_left` during the
 *   fetch re-fetches rather than dropping either side.
 * - Hydrates the selection from `voiceState` with the SNAPSHOT rule.
 * - Re-hydrates both on `control:connection` = `connected` (SyncClient fills
 *   the Go store during connect without per-client events), and re-reads
 *   `voiceState` on `profile:state` (a profile load or revert re-validates
 *   the selection in Go, with no selection event of its own).
 * - Follows `radio.<n>.select` hotkeys: Go applies that selection before it
 *   emits `hotkey:pressed`, so the event is the selection change.
 * - Clears everything on `auth:session_changed` = `logged_out`, including
 *   held PTT and the frozen global-PTT target, so a re-login under a new
 *   GUID never renders the old session's radios.
 */
export function useClientStateSync(): void {
  useEffect(() => {
    let client: HydrateHandle | null = null;
    let voice: HydrateHandle | null = null;

    const hydrateClient = () => {
      client?.dispose();
      client = hydrateDelta(
        () => api.getClientState(),
        applySnapshot,
        (err) => console.error("getClientState failed", err),
      );
    };
    const hydrateVoice = () => {
      voice?.dispose();
      voice = hydrateSnapshot(
        () => api.voiceState(),
        (vs) => useRadios.getState().setSelectedRadioId(vs.selected_radio),
        (err) => console.error("voiceState failed", err),
      );
    };

    hydrateClient();
    hydrateVoice();

    const offs = [
      on<RadioUpdatePayload>(EV.radioUpdate, (d) => {
        client?.pushed();
        useRadios.getState().setForGuid(d.guid, d.radio);
      }),
      on<ClientUpdatePayload>(EV.clientUpdate, (d) => {
        client?.pushed();
        useClients.getState().upsert(d.guid, d.info);
      }),
      on<ClientLeftPayload>(EV.clientLeft, (d) => {
        client?.pushed();
        useClients.getState().remove(d.guid);
      }),
      on<string>(EV.controlConnection, (c) => {
        if (c === "connected") {
          hydrateClient();
          hydrateVoice();
        }
      }),
      on<unknown>(EV.profileState, () => hydrateVoice()),
      on<string>(EV.authSession, (s) => {
        if (s !== "logged_out") return;
        client?.dispose();
        voice?.dispose();
        client = null;
        voice = null;
        useClients.getState().replaceAll({});
        useRadios.getState().reset();
        useSession.getState().setSelf(null);
        useSession.getState().setSelfGuid("");
      }),
      on<HotkeyEventPayload>(EV.hotkeyPressed, (d) => {
        const m = SELECT_RE.exec(d.action_id);
        if (!m) return;
        voice?.pushed();
        useRadios.getState().setSelectedRadioId(Number(m[1]));
      }),
    ];
    return () => {
      offs.forEach((off) => off());
      client?.dispose();
      voice?.dispose();
    };
  }, []);
}
