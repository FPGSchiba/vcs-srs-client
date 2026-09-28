import { create } from "zustand";

/** Mirrors Go's `notify.Severity`. Drives DELIVERY, not just colour:
 *  `error` toasts and sounds, `warn` reaches the badge and bell silently,
 *  and `info` is raised already-read by the backend so it reaches nothing
 *  at all. */
export type NotifySeverity = "error" | "warn" | "info";

/** One ordered context row, mirroring Go's `notify.KV`. An array rather
 *  than an object because a JSON object's key order is not display order --
 *  the same reason `AudioSettingsDTO.effect_order` exists. */
export interface NotifyKV {
  key: string;
  value: string;
}

/** One action button, mirroring Go's `notify.Action`. `kind` is a closed
 *  set so a new notification source adds no dispatch code here. */
export interface NotifyAction {
  label: string;
  icon: string;
  kind: "open-window" | "navigate";
  target: string;
  primary: boolean;
}

/** One notification, mirroring Go's `notify.Item`. `key` is "" for a
 *  discrete Post and non-empty for a raised condition. `time` is an RFC3339
 *  string -- Go's time.Time marshals that way. */
export interface NotifyItem {
  id: string;
  key: string;
  category: string;
  severity: NotifySeverity;
  icon: string;
  title: string;
  body: string;
  context: NotifyKV[];
  actions: NotifyAction[];
  time: string;
  unread: boolean;
  resolved: boolean;
}

/** The whole list plus the unread count, newest first. Mirrors Go's
 *  `notify.Snapshot`, which is broadcast in full on every change. */
export interface NotifySnapshot {
  items: NotifyItem[];
  unread: number;
}

export const emptySnapshot = (): NotifySnapshot => ({ items: [], unread: 0 });

interface NotificationsState {
  snap: NotifySnapshot;
  setSnapshot: (snap: NotifySnapshot) => void;
}

/**
 * The notification store is a MIRROR, never a source of truth. Go owns the
 * list (see internal/notify), because a popout is a separate webview with
 * its own JS heap: a list held here in the main window would be invisible
 * to the Notifications window. Every mutation round-trips through the
 * backend and comes back on `notifications:changed`.
 */
export const useNotifications = create<NotificationsState>((set) => ({
  snap: emptySnapshot(),
  setSnapshot: (snap) => set({ snap }),
}));
