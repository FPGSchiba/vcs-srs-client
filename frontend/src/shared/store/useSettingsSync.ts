import { useEffect } from "react";
import { api } from "../api/client";
import { on, EV } from "../api/events";
import { useSettings } from "./settings";
import type { Settings, Keybind, HotkeyState, JoystickState } from "./settings";

/**
 * useSettingsSync hydrates the shared settings store from the backend and
 * keeps it live for as long as the calling window is mounted.
 *
 * It belongs to a WINDOW, not to a screen. The subscriptions used to live in
 * `SettingsScreen`, which meant they were torn down the moment the user
 * navigated away from Settings, and the Comms popout — which never renders
 * that screen — never subscribed at all. The store sits in `shared/`
 * precisely because more than one window reads it (spec §9), so mount this
 * once per window shell (`MainApp`, `CommsApp`) and let screens just read
 * `useSettings`.
 *
 * Mounting it in several components of the same window is harmless but
 * pointless: each mount adds its own subscriptions and removes them on
 * unmount. One per window shell is the intent.
 */
export function useSettingsSync(): void {
  useEffect(() => {
    api
      .getSettings()
      .then((s) => useSettings.getState().setSettings(s))
      .catch(() => {
        /* not available yet — ignore */
      });
    api
      .getKeybinds()
      .then((k) => useSettings.getState().setKeybinds(k))
      .catch(() => {
        /* not available yet — ignore */
      });
    api
      .getHotkeyState()
      .then((h) => useSettings.getState().setHotkeyState(h))
      .catch((err) => {
        // Logged rather than swallowed: this is the one hydrate call whose
        // failure is otherwise completely invisible. The store's optimistic
        // `registered: true` default means a rejection here leaves the UI
        // claiming global hotkeys are fine when nothing has confirmed that,
        // so the console line is the only trace of why.
        console.error("getHotkeyState failed; hotkey health is unknown", err);
      });
    api
      .getJoystickState()
      .then((j) => useSettings.getState().setJoystickState(j))
      .catch((err) => {
        // Logged for the same reason as getHotkeyState above: the store's
        // honest `supported: false` default hides the capture affordance's
        // joystick half, so a rejection here silently leaves it hidden even
        // on a machine that does support it, with nothing but this line to
        // explain why.
        console.error("getJoystickState failed; joystick support is unknown", err);
      });

    const offs = [
      on<Settings>(EV.settingsChanged, (s) => useSettings.getState().setSettings(s)),
      on<Keybind[]>(EV.keybindsChanged, (k) => useSettings.getState().setKeybinds(k)),
      on<HotkeyState>(EV.hotkeysState, (h) => useSettings.getState().setHotkeyState(h)),
      // The joystick half of the same contract. The hydrate above runs once,
      // on mount, and the backend's view changes on its own afterwards: a
      // stick plugged in later, or a transient enumeration error clearing.
      // Without this subscription that first answer was the only one the UI
      // ever had, so a device attached after Settings mounted stayed invisible
      // and one bad poll pinned "Joystick unavailable" for the session.
      on<JoystickState>(EV.joystickState, (j) => useSettings.getState().setJoystickState(j)),
    ];
    return () => offs.forEach((off) => off());
  }, []);
}
