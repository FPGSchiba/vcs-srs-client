import { useEffect } from "react";
import { api } from "../../shared/api/client";
import type { RadioInfoDTO } from "../../shared/api/client";
import { on, EV } from "../../shared/api/events";
import { useRadios } from "../../shared/store/radios";
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
 * store optimistically).
 *
 * Phase-1 simplification: this window renders the first entry of the radios store.
 * If there are no radios it shows an empty state. The window chrome (title + close)
 * uses the ported `.popout`/`.popout-chrome` markup; close routes through the Go
 * window registry via api.closeWindow("comms"), which persists geometry and is
 * more reliable than the in-webview Window.Close().
 */
export function CommsApp() {
  const radios = useRadios((s) => s.radios);

  useEffect(() => {
    api
      .getClientState()
      .then((snap) => useRadios.getState().replaceAll(snap.radios ?? {}))
      .catch(() => {
        /* not connected yet — ignore */
      });

    const off = on<RadioUpdatePayload>(EV.radioUpdate, (d) =>
      useRadios.getState().setForGuid(d.guid, d.radio),
    );
    return () => off();
  }, []);

  const entry = Object.values(radios)[0];

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
