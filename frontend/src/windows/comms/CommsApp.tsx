import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../../shared/api/client";
import type { Layout, LayoutBlock, ProfileState, RadioInfoDTO } from "../../shared/api/client";
import { on, EV } from "../../shared/api/events";
import type { HotkeyEventPayload } from "../../shared/api/events";
import { useRadios } from "../../shared/store/radios";
import { useSession } from "../../shared/store/session";
import { useProfile } from "../../shared/store/profile";
import { GRID_GAP, GRID_PAD } from "../../shared/layout";
import { useSettingsSync } from "../../shared/store/useSettingsSync";
import { Icon } from "../../shared/components/Icon";
import { RadioBlock } from "./RadioBlock";
import { DEFAULT_VARIANT_ID } from "./variants";

/**
 * Renderer-side debounce for setCommsLayout. It writes no file (the bytes land
 * at shutdown); this only avoids an IPC call per pointer-move frame.
 */
const LAYOUT_DEBOUNCE_MS = 300;
const DEFAULT_WINDOW = { w: 540, h: 720 };

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

  const profile = useProfile((s) => s.state);

  // The stored layout: block order and sizes, plus the window size carried
  // through unchanged. Kept as the FULL stored list (including blocks for
  // radios not currently present) so saving never discards them.
  const [layout, setLayout] = useState<Layout>({ window: DEFAULT_WINDOW, blocks: [] });
  const layoutRef = useRef(layout);
  // The debounced save: `pending` is the layout waiting to be sent.
  const pending = useRef<Layout | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  const flush = useCallback(() => {
    clearTimeout(timer.current);
    timer.current = undefined;
    const l = pending.current;
    pending.current = null;
    if (l) void api.setCommsLayout(l);
  }, []);

  const commit = useCallback(
    (next: Layout) => {
      layoutRef.current = next;
      setLayout(next);
      pending.current = next;
      clearTimeout(timer.current);
      timer.current = setTimeout(flush, LAYOUT_DEBOUNCE_MS);
    },
    [flush],
  );

  // Hydrates from the backend. An edit that has not been sent yet is newer
  // than anything the backend can tell us, so it wins.
  const hydrateLayout = useCallback(() => {
    api
      .getCommsLayout()
      .then((l) => {
        if (pending.current) return;
        const next = { window: l.window ?? DEFAULT_WINDOW, blocks: l.blocks ?? [] };
        layoutRef.current = next;
        setLayout(next);
      })
      .catch(() => {
        /* not wired yet -- keep defaults */
      });
  }, []);

  // A window closed mid-drag must not lose its last edit. flush is idempotent
  // (it clears what it sends), so StrictMode's simulated unmount is harmless.
  useEffect(() => flush, [flush]);

  // profile:state fires for this window's own SetCommsLayout too, and for
  // revert / reset / profile load from anywhere. Re-reading on every event is
  // what makes the latter visible here; hydrateLayout's pending guard is what
  // keeps the former from fighting a drag.
  useEffect(() => {
    hydrateLayout();
    api
      .getProfileState()
      .then((s) => useProfile.getState().setState(s))
      .catch(() => {
        /* not wired yet -- ignore */
      });
    const off = on<ProfileState>(EV.profileState, (s) => {
      useProfile.getState().setState(s);
      hydrateLayout();
    });
    return () => off();
  }, [hydrateLayout]);

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

  // Display order: stored blocks that have a radio, in stored order, then any
  // radio without a block in the default variant.
  const shown: LayoutBlock[] = [];
  if (entry) {
    const present = new Set(entry.radios.map((r) => r.id));
    for (const b of layout.blocks) {
      if (present.has(b.radio_id) && !shown.some((x) => x.radio_id === b.radio_id)) shown.push(b);
    }
    for (const r of entry.radios) {
      if (!shown.some((x) => x.radio_id === r.id)) {
        shown.push({ radio_id: r.id, variant: DEFAULT_VARIANT_ID });
      }
    }
  }

  // The layout to persist for a new `shown`: blocks for radios that are not
  // present right now ride along at the end.
  function withShown(next: LayoutBlock[]): Layout {
    const ids = new Set(next.map((b) => b.radio_id));
    const absent = layoutRef.current.blocks.filter((b) => !ids.has(b.radio_id));
    return { window: layoutRef.current.window, blocks: [...next, ...absent] };
  }

  function resize(radioId: number, variant: string) {
    commit(withShown(shown.map((b) => (b.radio_id === radioId ? { ...b, variant } : b))));
  }

  function reorder(from: number, to: number) {
    if (from < 0 || from >= shown.length || to < 0 || to >= shown.length) return;
    const next = shown.slice();
    const [moved] = next.splice(from, 1);
    next.splice(to, 0, moved);
    commit(withShown(next));
  }

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
        <div className="row acenter gap-3" style={{ minWidth: 0 }}>
          <Icon name="broadcast" size={14} />
          <span className="ttl">Communications</span>
          {profile.active_name && (
            <span
              className="cap mono"
              style={{
                color: "var(--tx-3)",
                minWidth: 0,
                overflow: "hidden",
                textOverflow: "ellipsis",
                whiteSpace: "nowrap",
              }}
            >
              {profile.active_name}
            </span>
          )}
          {profile.dirty && (
            <span title="Unsaved layout changes" style={{ color: "var(--ac-primary)" }}>
              ●
            </span>
          )}
        </div>
        <span />
        <div className="ctrl">
          {profile.dirty && profile.active_path && (
            <button
              type="button"
              className="btn btn-sm btn-primary"
              title="Save changes into the active profile"
              onClick={() => {
                flush(); // an unsent drag must be part of what is saved
                void api.saveProfile();
              }}
            >
              SAVE
            </button>
          )}
          {profile.dirty && profile.active_path && (
            <button
              type="button"
              className="btn btn-sm"
              title="Discard changes and reload the active profile"
              onClick={() => void api.revertProfile()}
            >
              REVERT
            </button>
          )}
          <button
            type="button"
            className="btn btn-sm"
            title="Restore the default layout"
            onClick={() => void api.resetLayout()}
          >
            RESET
          </button>
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
          // Geometry here is the contract `place()` in shared/layout.ts
          // reproduces for the Profiles preview: wrap, GRID_PAD padding,
          // GRID_GAP gap, rows start at the top, blocks never shrink.
          <ul
            aria-label="Radios"
            style={{
              listStyle: "none",
              margin: 0,
              display: "flex",
              flexWrap: "wrap",
              alignContent: "flex-start",
              alignItems: "flex-start",
              padding: GRID_PAD,
              gap: GRID_GAP,
            }}
          >
            {shown.map((b, i) => {
              const r = entry.radios.find((x) => x.id === b.radio_id)!;
              return (
                <li key={r.id} style={{ flexShrink: 0 }}>
                  <RadioBlock
                    radio={r}
                    allRadios={entry.radios}
                    muted={entry.muted}
                    variantId={b.variant}
                    index={i}
                    onResize={resize}
                    onReorder={reorder}
                  />
                </li>
              );
            })}
          </ul>
        )}
      </div>
    </div>
  );
}
