import { useEffect } from "react";
import { api } from "../api/client";
import { on, EV } from "../api/events";
import { useSettings } from "./settings";
import { hydrateSnapshot } from "./hydrate";
import type {
  Settings,
  Keybind,
  HotkeyState,
  JoystickState,
  AudioDevices,
  AudioState,
  AudioVU,
} from "./settings";

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
    const st = () => useSettings.getState();
    // FE08: each snapshot channel is hydrated through hydrateSnapshot, and
    // its push handler marks the hydrate stale BEFORE it writes the store,
    // so a late hydrate result can never roll a pushed value back.
    const settingsH = hydrateSnapshot(
      () => api.getSettings(),
      (s) => st().setSettings(s),
      (err) => console.error("getSettings failed", err),
    );
    const keybindsH = hydrateSnapshot(
      () => api.getKeybinds(),
      (k) => st().setKeybinds(k),
      (err) => console.error("getKeybinds failed", err),
    );
    const hotkeysH = hydrateSnapshot(
      () => api.getHotkeyState(),
      (h) => st().setHotkeyState(h),
      // Logged rather than swallowed: this is the one hydrate call whose
      // failure is otherwise completely invisible. The store's optimistic
      // `registered: true` default means a rejection here leaves the UI
      // claiming global hotkeys are fine when nothing has confirmed that,
      // so the console line is the only trace of why.
      (err) => console.error("getHotkeyState failed; hotkey health is unknown", err),
    );
    const joystickH = hydrateSnapshot(
      () => api.getJoystickState(),
      (j) => st().setJoystickState(j),
      // Logged for the same reason as getHotkeyState above: the store's
      // honest `supported: false` default hides the capture affordance's
      // joystick half, so a rejection here silently leaves it hidden even
      // on a machine that does support it, with nothing but this line to
      // explain why.
      (err) => console.error("getJoystickState failed; joystick support is unknown", err),
    );
    const devicesH = hydrateSnapshot(
      () => api.getAudioDevices(),
      (d) => st().setAudioDevices(d),
      // The store's empty-list default already renders no devices.
      (err) => console.error("getAudioDevices failed", err),
    );
    const audioStateH = hydrateSnapshot(
      () => api.getAudioState(),
      (a) => st().setAudioState(a),
      // Logged for the same reason as getHotkeyState/getJoystickState
      // above: the store's honest `running: false` default already hides
      // any claim of health, so a rejection here silently leaves that in
      // place with nothing but this line to explain why.
      (err) => console.error("getAudioState failed; audio health is unknown", err),
    );
    // Static data (internal/audio's own preset tables), not per-Manager
    // state -- fetched once here, not re-pushed on any event, unlike
    // audioDevices/audioState above, so it needs no stale-hydrate guard.
    api
      .getAudioEffectPresets()
      .then((p) => st().setAudioEffectPresets(p))
      .catch((err) => {
        // The store's empty-lists default already renders no preset options.
        console.error("getAudioEffectPresets failed", err);
      });

    const handles = [settingsH, keybindsH, hotkeysH, joystickH, devicesH, audioStateH];
    const offs = [
      on<Settings>(EV.settingsChanged, (s) => {
        settingsH.pushed();
        st().setSettings(s);
      }),
      on<Keybind[]>(EV.keybindsChanged, (k) => {
        keybindsH.pushed();
        st().setKeybinds(k);
      }),
      on<HotkeyState>(EV.hotkeysState, (h) => {
        hotkeysH.pushed();
        st().setHotkeyState(h);
      }),
      // The joystick half of the same contract. The hydrate above runs once,
      // on mount, and the backend's view changes on its own afterwards: a
      // stick plugged in later, or a transient enumeration error clearing.
      // Without this subscription that first answer was the only one the UI
      // ever had, so a device attached after Settings mounted stayed invisible
      // and one bad poll pinned "Joystick unavailable" for the session.
      on<JoystickState>(EV.joystickState, (j) => {
        joystickH.pushed();
        st().setJoystickState(j);
      }),
      on<AudioDevices>(EV.audioDevicesChanged, (d) => {
        devicesH.pushed();
        st().setAudioDevices(d);
      }),
      on<AudioState>(EV.audioState, (a) => {
        audioStateH.pushed();
        st().setAudioState(a);
      }),
      on<AudioVU>(EV.audioVU, (v) => st().setVU(v)),
      on<boolean>(EV.audioMicMuted, (m) => st().setMicMuted(m)),
    ];
    return () => {
      offs.forEach((off) => off());
      handles.forEach((h) => h.dispose());
    };
  }, []);
}
