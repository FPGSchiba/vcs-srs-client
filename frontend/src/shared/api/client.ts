import { App } from "../../../bindings/github.com/FPGSchiba/vcs-srs-client/internal/app";
import { Settings, Keybind, HotkeyState, Capture, SetKeybindResult } from "../store/settings";

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
  beginCapture: (): Promise<void> => App.BeginCapture() as Promise<void>,
  endCapture: (): Promise<void> => App.EndCapture() as Promise<void>,
  getHotkeyState: (): Promise<HotkeyState> => App.GetHotkeyState() as Promise<HotkeyState>,
};
