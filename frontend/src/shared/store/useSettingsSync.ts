import { useEffect } from "react";
import { api } from "../api/client";
import { on, EV } from "../api/events";
import { useSettings } from "./settings";
import type { Settings, Keybind, HotkeyState } from "./settings";

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

    const offs = [
      on<Settings>(EV.settingsChanged, (s) => useSettings.getState().setSettings(s)),
      on<Keybind[]>(EV.keybindsChanged, (k) => useSettings.getState().setKeybinds(k)),
      on<HotkeyState>(EV.hotkeysState, (h) => useSettings.getState().setHotkeyState(h)),
    ];
    return () => offs.forEach((off) => off());
  }, []);
}
