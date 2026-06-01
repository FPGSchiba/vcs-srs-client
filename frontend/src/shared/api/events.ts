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
} as const;

// on subscribes to a Wails event and returns an unsubscribe function.
export function on<T = unknown>(name: string, cb: (data: T) => void): () => void {
  const off = Events.On(name, (e: { data: T }) => cb(e.data));
  return off;
}
