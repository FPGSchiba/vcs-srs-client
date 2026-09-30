import { useEffect, useRef, useState } from "react";
import { Icon } from "./Icon";
import { useNotifications, type NotifyItem } from "../store/notifications";

/** How long a toast stays up. The design prototype's value
 *  (`design/vcs/project/app.jsx:234`). */
const DISMISS_MS = 6500;

/**
 * `signature` is this file's local answer to "is this the same notification
 * or a different one under the same id?".
 *
 * It exists because a keyed `Raise` whose fingerprint DIFFERS updates the
 * item IN PLACE, keeping the id (see Go's `notify.raiseLocked`): the store
 * re-marks it unread and plays the sound, so gating the toast on the id
 * alone produced a sound with nothing new on screen -- exactly what spec
 * 3.8's "it fires on exactly the same items the toast does. One rule, two
 * surfaces." rules out. It is reachable on a path the spec itself names
 * (5.1's `denied -> granted-but-still-unregistered` transition updates
 * `hotkeys.global` in place) and on `audio.input` whenever malgo's error
 * text differs between attempts.
 *
 * Deliberately the same NOTION of change as Go's `fingerprint` -- severity,
 * title, body, ordered context, actions; never id, time, unread or resolved
 * -- but not the same VALUE. Porting the sha256 hasher would couple a React
 * component to a Go implementation detail for no gain: nothing compares the
 * two, this one only ever compares a signature with an earlier signature of
 * its own making.
 */
function signature(i: NotifyItem): string {
  return JSON.stringify([i.severity, i.title, i.body, i.context, i.actions]);
}

/**
 * ToastHost surfaces ERROR-severity notifications transiently, so a failure
 * reaches a user who is looking at something other than the Notifications
 * popout.
 *
 * Without it, replacing Phase 3's inline Keybinds banner with a popout would
 * be a regression: the failure used to be visible the moment you opened
 * Keybinds, and would otherwise become invisible unless you opened a window
 * you have no reason to open.
 *
 * Error only, deliberately -- sound follows the same rule (see Go's
 * App.notificationSFXID). A warn item reaches the badge and bell silently,
 * and an info item is raised already-read by the backend so it reaches
 * nothing at all. That is what makes macOS's "joystick unsupported"
 * structurally incapable of rendering as a failure.
 *
 * Mounted in the MAIN window only. Comms is a narrow radio panel and a toast
 * stack would cover the radios -- a deliberate limit, not an oversight.
 */
export function ToastHost() {
  const items = useNotifications((s) => s.snap.items);
  const [visible, setVisible] = useState<NotifyItem[]>([]);

  // What each id was last toasted AS, keyed id -> signature. A ref, not
  // state: a full Snapshot is broadcast on EVERY change -- including changes
  // to other items -- so without this each republish would re-toast every
  // error still in the list.
  //
  // The signature, not a bare id set, is what keeps this host and the sound
  // firing on the same items: an in-place content change keeps the id and
  // must re-toast. See `signature`.
  const seen = useRef<Map<string, string>>(new Map());

  // Ids whose toast is OFF SCREEN although the item is still live: dismissed
  // by hand, or expired naturally after its dwell.
  //
  // Tracked explicitly because the repair clause below re-arms a timer for
  // anything toasted, present and unresolved -- which, without this, silently
  // included an item whose toast was already gone, re-arming a phantom 6.5s
  // timer on every subsequent items change, each firing a `setVisible` that
  // allocates and re-renders for no visual change.
  //
  // BOTH exits must record here. Hand-dismissal was covered from the start;
  // natural expiry was not, and it leaks the same way: the timer deletes
  // itself from `timers` but the id stays in `seen`, so the very next `items`
  // change armed a fresh timer for an invisible item.
  //
  // An entry is dropped the moment the item's content changes (see the
  // `fresh` loop): that is a new occurrence, and what retired was the old
  // one.
  const retired = useRef<Set<string>>(new Set());

  // Live dismiss timers, keyed by item id. A ref so the cleanup can clear
  // them without the effect depending on them and rescheduling.
  const timers = useRef<Map<string, ReturnType<typeof setTimeout>>>(new Map());

  const clearTimer = (id: string) => {
    const t = timers.current.get(id);
    if (t !== undefined) {
      clearTimeout(t);
      timers.current.delete(id);
    }
  };

  const dismiss = (id: string) => {
    clearTimer(id);
    retired.current.add(id);
    setVisible((cur) => cur.filter((i) => i.id !== id));
  };

  useEffect(() => {
    // Everything the backend still considers live. An item that has RESOLVED
    // or dropped out of the list must not keep its toast up for the rest of
    // the dwell: a microphone error that clears a second later would
    // otherwise sit on screen for another 5.5s claiming a fault that is
    // already gone.
    const live = new Set(items.filter((i) => !i.resolved).map((i) => i.id));
    for (const id of [...timers.current.keys()]) {
      if (!live.has(id)) clearTimer(id);
    }

    // Prune both bookkeeping refs against the list. Neither is otherwise ever
    // shrunk, so both grow for the lifetime of the main window -- including
    // ids the backend evicted long ago under its 200-item cap. That is
    // bounded today, but only accidentally: 7.3/7.4 add per-client and
    // per-frequency notifications, at which point the growth is driven by
    // remote traffic. Safe to drop an absent id because ids are never reused:
    // the backend assigns them from a monotonic counter (Go's
    // `Notifier.nextIDLocked`), and an item that leaves the list -- dismissed
    // backend-side, cleared, or evicted -- can only come back as a NEW
    // occurrence under a NEW id, which is a re-toast in any case.
    const present = new Set(items.map((i) => i.id));
    for (const id of [...seen.current.keys()]) {
      if (!present.has(id)) seen.current.delete(id);
    }
    for (const id of [...retired.current]) {
      if (!present.has(id)) retired.current.delete(id);
    }

    const fresh = items.filter(
      (i) => i.severity === "error" && !i.resolved && seen.current.get(i.id) !== signature(i),
    );
    for (const i of fresh) {
      seen.current.set(i.id, signature(i));
      // A re-toast is a new occurrence: it gets the full dwell, and it
      // overrides the PREVIOUS content's retirement, however that toast left
      // the screen.
      clearTimer(i.id);
      retired.current.delete(i.id);
    }

    // Restore the invariant "every item this host has ever toasted, that is
    // still present and unresolved and whose toast has not already retired,
    // has a live dismiss timer" -- rather than assuming it holds. StrictMode mounts,
    // cleans up and remounts every effect: the cleanup below can clear a
    // just-created timer before this effect's remount runs, and since `seen`
    // is a ref that survives that cycle, the `fresh` filter above would skip
    // the item on remount and it would never get its timer back. Driving the
    // repair off `items` + `seen` (both already correct within this same
    // invocation) rather than the `visible` STATE sidesteps a further
    // hazard: `visible` may not yet reflect a `setVisible` call queued
    // earlier in this very effect run.
    const stillToasted = items.filter(
      (i) => seen.current.has(i.id) && !i.resolved && !retired.current.has(i.id),
    );
    for (const i of stillToasted) {
      // Guard against a double-schedule. A second timer for the same id
      // would fire into a toast a later item may already have replaced.
      if (timers.current.has(i.id)) continue;
      timers.current.set(
        i.id,
        setTimeout(() => {
          timers.current.delete(i.id);
          // Record the natural expiry, exactly as `dismiss` records a hand
          // dismissal: the item is still live backend-side, so without this
          // the repair clause above would re-arm a timer for a toast that is
          // no longer on screen, on every subsequent items change.
          retired.current.add(i.id);
          setVisible((cur) => cur.filter((x) => x.id !== i.id));
        }, DISMISS_MS),
      );
    }

    setVisible((cur) => {
      // Retraction, then replacement-or-append. A re-toasted item replaces
      // its own entry rather than stacking a second one: `key={t.id}` in the
      // render below would otherwise be duplicated.
      const kept = cur.filter((v) => live.has(v.id));
      if (fresh.length === 0) {
        // Returning the same reference when nothing changed lets React bail
        // out, so a republished-but-identical snapshot causes no re-render.
        return kept.length === cur.length ? cur : kept;
      }
      const freshIDs = new Set(fresh.map((i) => i.id));
      return [...kept.filter((v) => !freshIDs.has(v.id)), ...fresh];
    });
  }, [items]);

  // Real-unmount cleanup, kept SEPARATE from the effect above so it runs
  // once on unmount rather than on every items change. Clearing timers in
  // the subscribing effect's cleanup would cancel a live toast's dismiss
  // every time an unrelated notification arrived.
  useEffect(() => {
    const live = timers.current;
    return () => {
      for (const t of live.values()) clearTimeout(t);
      live.clear();
    };
  }, []);

  if (visible.length === 0) return null;

  return (
    <div className="toast-stack">
      {visible.map((t) => (
        <div key={t.id} className="toast alert">
          <div className="row between acenter">
            <span style={{ fontSize: 12, color: "var(--tx-0)", overflowWrap: "anywhere" }}>
              {t.title}
            </span>
            <button
              type="button"
              className="btn btn-ghost btn-icon btn-sm"
              aria-label={`Dismiss ${t.title}`}
              onClick={() => dismiss(t.id)}
            >
              <Icon name="x" size={10} />
            </button>
          </div>
          {t.body && (
            <div
              style={{
                fontSize: 11,
                color: "var(--tx-2)",
                marginTop: 4,
                textWrap: "pretty",
                overflowWrap: "anywhere",
              }}
            >
              {t.body}
            </div>
          )}
        </div>
      ))}
    </div>
  );
}
