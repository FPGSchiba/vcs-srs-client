import { useEffect } from "react";
import { api } from "../../shared/api/client";
import type { RadioInfoDTO } from "../../shared/api/client";
import { on, EV } from "../../shared/api/events";
import type { HotkeyEventPayload } from "../../shared/api/events";
import { useRadios } from "../../shared/store/radios";
import { useSession } from "../../shared/store/session";
import { useSettingsSync } from "../../shared/store/useSettingsSync";
import { Icon } from "../../shared/components/Icon";
import { RadioCard } from "./RadioCard";

interface RadioUpdatePayload {
  guid: string;
  radio: RadioInfoDTO;
}

/**
 * CommsApp is the Comms pop-out window shell. On mount it hydrates the radios
 * store from a one-shot snapshot and subscribes to the backend `state:radio_update`
 * echo, mapping each into the radios Zustand store (the store is the single source
 * of truth — RadioCard edits round-trip through the server, never mutating the
 * store optimistically). It also mounts `useSettingsSync`, so the shared
 * settings/keybind store stays live in this window for as long as it is open.
 *
 * Renders the LOCAL client's radios: `radios[selfGuid]`, keyed by the
 * session's own GUID resolved from the client-state snapshot. (Previously
 * this rendered `Object.values(radios)[0]` -- the first entry of the WHOLE
 * radios map, which is any client's radios, not necessarily ours. Harmless
 * while nobody else was connected; wrong the moment voice makes
 * multi-client sessions real.) If there are no radios it shows an empty
 * state. The window chrome (title + close) uses the ported
 * `.popout`/`.popout-chrome` markup; close routes through the Go window
 * registry via api.closeWindow("comms"), which persists geometry and is
 * more reliable than the in-webview Window.Close().
 *
 * Also hydrates the selected-radio id (api.voiceState) and subscribes to
 * hotkey:pressed/released to track which PTT actions are currently held,
 * both of which RadioCard needs to show live transmit state.
 */
export function CommsApp() {
  const radios = useRadios((s) => s.radios);
  const selfGuid = useSession((s) => s.selfGuid);

  // Subscribes this window to settings:changed / keybinds:changed /
  // hotkeys:state. Without it the popout only ever saw the state it was
  // opened with, and a settings or keybind change made in the main window
  // required closing and reopening it (spec DoD 10).
  useSettingsSync();

  useEffect(() => {
    api
      .getClientState()
      .then((snap) => {
        useRadios.getState().replaceAll(snap.radios ?? {});
        useSession.getState().setSelfGuid(snap.self_guid ?? "");
      })
      .catch(() => {
        /* not connected yet — ignore */
      });

    api
      .voiceState()
      .then((vs) => useRadios.getState().setSelectedRadioId(vs.selected_radio))
      .catch(() => {
        /* not connected yet — ignore */
      });

    const offs = [
      on<RadioUpdatePayload>(EV.radioUpdate, (d) =>
        useRadios.getState().setForGuid(d.guid, d.radio),
      ),
      on<HotkeyEventPayload>(EV.hotkeyPressed, (d) =>
        useRadios.getState().setPTTHeld(d.action_id, true),
      ),
      on<HotkeyEventPayload>(EV.hotkeyReleased, (d) =>
        useRadios.getState().setPTTHeld(d.action_id, false),
      ),
    ];
    return () => offs.forEach((off) => off());
  }, []);

  const entry = selfGuid ? radios[selfGuid] : undefined;

  return (
    <div
      className="popout"
      style={{
        position: "static",
        width: "100%",
        height: "100%",
        border: "none",
        borderRadius: 0,
        boxShadow: "none",
        minWidth: 0,
        minHeight: 0,
      }}
    >
      <div className="popout-chrome">
        <Icon name="broadcast" size={14} />
        <span className="ttl">Communications</span>
        <div className="ctrl">
          <button
            type="button"
            className="close"
            aria-label="close"
            title="Close"
            onClick={() => void api.closeWindow("comms")}
          >
            <Icon name="close" size={14} />
          </button>
        </div>
      </div>

      <div className="popout-body">
        {!entry || entry.radios.length === 0 ? (
          <div
            className="col acenter"
            style={{ justifyContent: "center", height: "100%", color: "var(--tx-3)", gap: 8 }}
          >
            No radios — connect first
          </div>
        ) : (
          <div className="col gap-4" style={{ padding: 12 }}>
            {entry.radios.map((r) => (
              <RadioCard key={r.id} radio={r} allRadios={entry.radios} muted={entry.muted} />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
