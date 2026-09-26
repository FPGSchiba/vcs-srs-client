import { Events } from "@wailsio/runtime";

// Event names — MUST match internal/events/events.go constants.
export const EV = {
  clientState: "state:client_state",
  clientUpdate: "state:client_update",
  clientLeft: "state:client_left",
  radioUpdate: "state:radio_update",
  settingsUpdate: "state:settings_update",
  serverAction: "state:server_action",
  authSession: "auth:session_changed",
  controlConnection: "control:connection",
  windowState: "window:state",
  settingsChanged: "settings:changed",
  keybindsChanged: "keybinds:changed",
  hotkeyPressed: "hotkey:pressed",
  hotkeyReleased: "hotkey:released",
  hotkeysState: "hotkeys:state",
  joystickCaptured: "keybinds:joy_captured",
  joystickState: "joystick:state",
  captureExpired: "keybinds:capture_expired",
  audioDevicesChanged: "audio:devices_changed",
  audioVU: "audio:vu",
  audioState: "audio:state",
  audioMicMuted: "audio:mic_muted",
  connectionState: "connection:state",
  // Emitted by the backend since Phase 5's I2 fix, and subscribed by
  // nothing until now — the whole voice.Session state machine had no
  // frontend consumer at all.
  voiceState: "voice:state",
  voiceAddressUpdate: "voice:address_update",
} as const;

// HotkeyEventPayload is the payload shape for EV.hotkeyPressed /
// EV.hotkeyReleased -- mirrors internal/events.HotkeyPressed/HotkeyReleased's
// `{ action_id }` envelope. actionId is a keybind action id such as
// "global.ptt" or "radio.3.ptt" (see internal/app/voice.go's
// resolveTXTarget / perRadioPTTID).
export interface HotkeyEventPayload {
  action_id: string;
}

// on subscribes to a Wails event and returns an unsubscribe function.
export function on<T = unknown>(name: string, cb: (data: T) => void): () => void {
  const off = Events.On(name, (e: { data: T }) => cb(e.data));
  return off;
}
