import { useEffect, useRef, useState } from "react";
import { Icon } from "./Icon";
import { useNotifications, type NotifyItem } from "../store/notifications";

/** How long a toast stays up. The design prototype's value
 *  (`design/vcs/project/app.jsx:234`). */
const DISMISS_MS = 6500;

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

  // Ids already toasted. A ref, not state: a full Snapshot is broadcast on
  // EVERY change -- including changes to other items -- so without this each
  // republish would re-toast every error still in the list.
  const seen = useRef<Set<string>>(new Set());

  // Live dismiss timers, keyed by item id. A ref so the cleanup can clear
  // them without the effect depending on them and rescheduling.
  const timers = useRef<Map<string, ReturnType<typeof setTimeout>>>(new Map());

  const dismiss = (id: string) => {
    const t = timers.current.get(id);
    if (t !== undefined) {
      clearTimeout(t);
      timers.current.delete(id);
    }
    setVisible((cur) => cur.filter((i) => i.id !== id));
  };

  useEffect(() => {
    const fresh = items.filter(
      (i) => i.severity === "error" && !i.resolved && !seen.current.has(i.id),
    );
    for (const i of fresh) seen.current.add(i.id);

    // Restore the invariant "every item this host has ever toasted, that is
    // still present and unresolved, has a live dismiss timer" -- rather than
    // assuming it holds. StrictMode mounts, cleans up and remounts every
    // effect: the cleanup below can clear a just-created timer before this
    // effect's remount runs, and since `seen` is a ref that survives that
    // cycle, the `fresh` filter above would skip the item on remount and it
    // would never get its timer back. Driving the repair off `items` +
    // `seen` (both already correct within this same invocation) rather than
    // the `visible` STATE sidesteps a further hazard: `visible` may not yet
    // reflect a `setVisible` call queued earlier in this very effect run.
    const stillToasted = items.filter((i) => seen.current.has(i.id) && !i.resolved);
    for (const i of stillToasted) {
      // Guard against a double-schedule. A second timer for the same id
      // would fire into a toast a later item may already have replaced.
      if (timers.current.has(i.id)) continue;
      timers.current.set(
        i.id,
        setTimeout(() => {
          timers.current.delete(i.id);
          setVisible((cur) => cur.filter((x) => x.id !== i.id));
        }, DISMISS_MS),
      );
    }

    if (fresh.length > 0) {
      setVisible((cur) => [...cur, ...fresh]);
    }
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
