import { useEffect } from "react";
import { api } from "../api/client";
import { on, EV } from "../api/events";
import { useNotifications, type NotifySnapshot } from "./notifications";

/**
 * useNotificationsSync hydrates the notification store from the backend and
 * keeps it live for as long as the calling window is mounted.
 *
 * It belongs to a WINDOW, not a screen -- the same contract
 * `useSettingsSync` documents. Mount it once per window shell that has a
 * notification surface: `MainApp` (badge, bell, toasts) and
 * `NotificationsApp` (the list). Comms has no notification surface and does
 * not mount it.
 */
export function useNotificationsSync(): void {
  useEffect(() => {
    api
      .getNotifications()
      .then((s) => useNotifications.getState().setSnapshot(s))
      .catch((err) => {
        // Logged rather than swallowed, for the same reason
        // getHotkeyState's rejection is: the store's empty default renders
        // an honest "all clear", so a failure here is otherwise completely
        // invisible -- the UI would claim there is nothing to report when
        // nothing has confirmed that.
        console.error("getNotifications failed; the notification list is unknown", err);
      });

    const off = on<NotifySnapshot>(EV.notifications, (s) =>
      useNotifications.getState().setSnapshot(s),
    );
    return off;
  }, []);
}
