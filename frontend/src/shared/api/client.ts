import { App } from "../../../bindings/github.com/FPGSchiba/vcs-srs-client/internal/app";
import type {
  Settings,
  Keybind,
  HotkeyState,
  HotkeyPermissionResult,
  Capture,
  SetKeybindResult,
  JoystickState,
} from "../store/settings";

export interface RadioDTO {
  id: number;
  name: string;
  frequency: number;
  enabled: boolean;
  is_intercom: boolean;
}
export interface RadioInfoDTO {
  radios: RadioDTO[];
  muted: boolean;
}
export interface ClientInfoDTO {
  name: string;
  coalition: string;
  unit_id: string;
  role_id: number;
}
export interface ClientStateSnapshot {
  clients: Record<string, ClientInfoDTO>;
  radios: Record<string, RadioInfoDTO>;
  self_guid: string;
  self: ClientInfoDTO | null;
}
export interface BuildInfo {
  client_version: string;
  protocol_version: string;
  build: string;
}

export const api = {
  getBuildInfo: (): Promise<BuildInfo> => App.GetBuildInfo() as Promise<BuildInfo>,
  connect: (server: string, name: string, password: string, unitId: string) =>
    App.Connect(server, name, password, unitId),
  disconnect: () => App.Disconnect(),
  reconnect: () => App.Reconnect(),
  getClientState: (): Promise<ClientStateSnapshot> =>
    App.GetClientState() as Promise<ClientStateSnapshot>,
  updateRadioInfo: (info: RadioInfoDTO) => App.UpdateRadioInfo(info),
  openWindow: (id: string) => App.OpenWindow(id),
  closeWindow: (id: string) => App.CloseWindow(id),
  toggleWindow: (id: string) => App.ToggleWindow(id),
  getOpenWindows: (): Promise<string[]> => App.GetOpenWindows() as Promise<string[]>,
  getSettings: (): Promise<Settings> => App.GetSettings() as Promise<Settings>,
  setSettings: (s: Settings): Promise<void> => App.SetSettings(s) as Promise<void>,
  getKeybinds: (): Promise<Keybind[]> => App.GetKeybinds() as Promise<Keybind[]>,
  addTrigger: (actionId: string, cap: Capture): Promise<SetKeybindResult> =>
    App.AddTrigger(actionId, cap) as Promise<SetKeybindResult>,
  removeTrigger: (actionId: string, index: number): Promise<void> =>
    App.RemoveTrigger(actionId, index) as Promise<void>,
  clearKeybind: (actionId: string): Promise<void> =>
    App.ClearKeybind(actionId) as Promise<void>,
  // beginCapture returns a CAPTURE TOKEN that must be handed back to
  // endCapture. The backend only re-arms OS hotkeys for the token of the
  // capture that is still current, which is what makes switching rows
  // mid-capture safe without the frontend ordering two IPC calls.
  //
  // Takes the action id because a completed joystick capture is bound by the
  // backend itself, which therefore has to know which action it belongs to
  // (unlike a keyboard chord, which still round-trips through addTrigger).
  beginCapture: (actionId: string): Promise<number> =>
    App.BeginCapture(actionId) as Promise<number>,
  endCapture: (token: number): Promise<void> => App.EndCapture(token) as Promise<void>,
  getHotkeyState: (): Promise<HotkeyState> => App.GetHotkeyState() as Promise<HotkeyState>,
  getJoystickState: (): Promise<JoystickState> =>
    App.GetJoystickState() as Promise<JoystickState>,
  // requestHotkeyPermission fires the OS prompt for global hotkey capture
  // (macOS Accessibility trust). The resolved `prompted` is NOT a grant signal
  // -- see HotkeyPermissionResult. A grant arrives later on hotkeys:state,
  // via the backend's bounded re-check or its window-focus re-check.
  requestHotkeyPermission: (): Promise<HotkeyPermissionResult> =>
    App.RequestHotkeyPermission() as Promise<HotkeyPermissionResult>,
  openHotkeyPermissionSettings: (): Promise<void> =>
    App.OpenHotkeyPermissionSettings() as Promise<void>,
  // recheckHotkeyPermission forces the re-check the window-focus hook does
  // automatically, for a user who granted access and wants an answer now.
  // Re-applies the OS registrations and emits hotkeys:state if it flipped.
  recheckHotkeyPermission: (): Promise<void> =>
    App.RecheckHotkeyPermission() as Promise<void>,
};
