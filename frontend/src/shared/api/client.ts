import { App } from "../../../bindings/github.com/FPGSchiba/vcs-srs-client/internal/app";

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
};
