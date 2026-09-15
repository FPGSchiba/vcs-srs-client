import { App } from "../../../bindings/github.com/FPGSchiba/vcs-srs-client/internal/app";
import type { Settings, Keybind, HotkeyState, Capture, SetKeybindResult } from "../store/settings";

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
  setKeybind: (actionId: string, cap: Capture): Promise<SetKeybindResult> =>
    App.SetKeybind(actionId, cap) as Promise<SetKeybindResult>,
  clearKeybind: (actionId: string): Promise<void> =>
    App.ClearKeybind(actionId) as Promise<void>,
  // beginCapture returns a CAPTURE TOKEN that must be handed back to
  // endCapture. The backend only re-arms OS hotkeys for the token of the
  // capture that is still current, which is what makes switching rows
  // mid-capture safe without the frontend ordering two IPC calls.
  beginCapture: (): Promise<number> => App.BeginCapture() as Promise<number>,
  endCapture: (token: number): Promise<void> => App.EndCapture(token) as Promise<void>,
  getHotkeyState: (): Promise<HotkeyState> => App.GetHotkeyState() as Promise<HotkeyState>,
};
