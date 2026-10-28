import { App } from "../../../bindings/github.com/FPGSchiba/vcs-srs-client/internal/app";
import type * as models from "../../../bindings/github.com/FPGSchiba/vcs-srs-client/internal/app/models";
import type * as notify from "../../../bindings/github.com/FPGSchiba/vcs-srs-client/internal/notify/models";
import type {
  Settings,
  AudioSettings,
  AudioLevels,
  AudioEffect,
  Keybind,
  Trigger,
  Stolen,
  HotkeyState,
  HotkeyPermissionResult,
  Capture,
  SetKeybindResult,
  JoystickState,
  AudioDevices,
  AudioState,
  AudioEffectPresets,
} from "../store/settings";
import type { ConnectionState } from "../store/connection";
import type { NotifySnapshot, NotifyItem, NotifyKV, NotifyAction } from "../store/notifications";

export type RadioDTO = models.RadioDTO;
export type RadioInfoDTO = models.RadioInfoDTO;
export type ClientInfoDTO = models.ClientInfoDTO;
/** Frontend-shaped on purpose: Wails emits Go maps as `{[k]?: T}`, and every
 * consumer relies on non-optional map values. Pinned to the generated
 * `models.ClientStateSnapshot` by a `SameKeys` guard below. */
export interface ClientStateSnapshot {
  clients: Record<string, ClientInfoDTO>;
  radios: Record<string, RadioInfoDTO>;
  self_guid: string;
  self: ClientInfoDTO | null;
}
export type BuildInfo = models.BuildInfoDTO;
export type VoiceStateDTO = models.VoiceStateDTO;
export type LayoutBlock = models.ProfileBlockDTO;
export type LayoutWindow = models.ProfileWindowDTO;
export type Layout = models.LayoutDTO;
export type ProfileSummary = models.ProfileSummaryDTO;
export type ProfileState = models.ProfileStateDTO;
export type HistoryEntry = models.HistoryEntryDTO;

/**
 * True iff A and B have exactly the same property names. Used as a
 * compile-time drift guard: `const _x: SameKeys<Generated, Frontend> = true`
 * fails `tsc` ("Type 'true' is not assignable to type 'false'") the moment a
 * Go DTO field is added, removed or renamed without the frontend type
 * following.
 */
type SameKeys<A, B> = [Exclude<keyof A, keyof B>, Exclude<keyof B, keyof A>] extends [never, never]
  ? true
  : false;

/**
 * narrow re-types a binding call's result as its frontend type. It is the one
 * cast in this file, and it is valid ONLY for a call whose generated and
 * frontend types (and every DTO nested in them) are pinned by a `SameKeys`
 * guard below. What it absorbs is widening the generator cannot express:
 * Go maps arrive as `{[k: string]?: T}` where the frontend uses
 * `Record<string, T>`, and Go strings where the frontend uses literal unions
 * (trigger kind, hotkey permission, notification severity). Every other call
 * is un-cast, so its type is checked against the generated binding.
 */
function narrow<Fe>(p: Promise<unknown>): Promise<Fe> {
  return p as Promise<Fe>;
}

// Drift guards for every narrowed call and every DTO nested in it. Each
// field's type is `true` only while the generated and frontend key sets
// match; on drift it becomes `false` and the `true` below fails `tsc`.
const _driftGuards: {
  clientStateSnapshot: SameKeys<models.ClientStateSnapshot, ClientStateSnapshot>;
  settings: SameKeys<models.SettingsDTO, Settings>;
  audioSettings: SameKeys<models.AudioSettingsDTO, AudioSettings>;
  audioLevels: SameKeys<models.AudioLevelsDTO, AudioLevels>;
  audioEffect: SameKeys<models.AudioEffectDTO, AudioEffect>;
  keybind: SameKeys<models.KeybindDTO, Keybind>;
  trigger: SameKeys<models.TriggerDTO, Trigger>;
  setKeybindResult: SameKeys<models.SetKeybindResult, SetKeybindResult>;
  stolen: SameKeys<models.StolenDTO, Stolen>;
  hotkeyState: SameKeys<models.HotkeyStateDTO, HotkeyState>;
  hotkeyPermissionResult: SameKeys<models.HotkeyPermissionResultDTO, HotkeyPermissionResult>;
  notifySnapshot: SameKeys<notify.Snapshot, NotifySnapshot>;
  notifyItem: SameKeys<notify.Item, NotifyItem>;
  notifyKV: SameKeys<notify.KV, NotifyKV>;
  notifyAction: SameKeys<notify.Action, NotifyAction>;
} = {
  clientStateSnapshot: true,
  settings: true,
  audioSettings: true,
  audioLevels: true,
  audioEffect: true,
  keybind: true,
  trigger: true,
  setKeybindResult: true,
  stolen: true,
  hotkeyState: true,
  hotkeyPermissionResult: true,
  notifySnapshot: true,
  notifyItem: true,
  notifyKV: true,
  notifyAction: true,
};
void _driftGuards;

export const api = {
  getBuildInfo: (): Promise<BuildInfo> => App.GetBuildInfo(),
  connect: (server: string, name: string, password: string, unitId: string) =>
    App.Connect(server, name, password, unitId),
  disconnect: () => App.Disconnect(),
  reconnect: () => App.Reconnect(),
  cancelConnect: (): Promise<void> => App.CancelConnect(),
  getLastServerAddress: (): Promise<string> => App.GetLastServerAddress(),
  getClientState: (): Promise<ClientStateSnapshot> =>
    narrow<ClientStateSnapshot>(App.GetClientState()),
  updateRadioInfo: (info: RadioInfoDTO) => App.UpdateRadioInfo(info),
  selectRadio: (id: number): Promise<void> => App.SelectRadio(id),
  voiceState: (): Promise<VoiceStateDTO> => App.VoiceState(),
  openWindow: (id: string) => App.OpenWindow(id),
  closeWindow: (id: string) => App.CloseWindow(id),
  toggleWindow: (id: string) => App.ToggleWindow(id),
  getOpenWindows: (): Promise<string[]> => App.GetOpenWindows(),
  getSettings: (): Promise<Settings> => narrow<Settings>(App.GetSettings()),
  setSettings: (s: Settings): Promise<void> => App.SetSettings(s),
  getKeybinds: (): Promise<Keybind[]> => narrow<Keybind[]>(App.GetKeybinds()),
  addTrigger: (actionId: string, cap: Capture): Promise<SetKeybindResult> =>
    narrow<SetKeybindResult>(App.AddTrigger(actionId, cap)),
  removeTrigger: (actionId: string, index: number): Promise<void> =>
    App.RemoveTrigger(actionId, index),
  clearKeybind: (actionId: string): Promise<void> =>
    App.ClearKeybind(actionId),
  // beginCapture returns a CAPTURE TOKEN that must be handed back to
  // endCapture. The backend only re-arms OS hotkeys for the token of the
  // capture that is still current, which is what makes switching rows
  // mid-capture safe without the frontend ordering two IPC calls.
  //
  // Takes the action id because a completed joystick capture is bound by the
  // backend itself, which therefore has to know which action it belongs to
  // (unlike a keyboard chord, which still round-trips through addTrigger).
  beginCapture: (actionId: string): Promise<number> =>
    App.BeginCapture(actionId),
  endCapture: (token: number): Promise<void> => App.EndCapture(token),
  getHotkeyState: (): Promise<HotkeyState> => narrow<HotkeyState>(App.GetHotkeyState()),
  getJoystickState: (): Promise<JoystickState> =>
    App.GetJoystickState(),
  // requestHotkeyPermission fires the OS prompt for global hotkey capture
  // (macOS Accessibility trust). The resolved `prompted` is NOT a grant signal
  // -- see HotkeyPermissionResult. A grant arrives later on hotkeys:state,
  // via the backend's bounded re-check or its window-focus re-check.
  requestHotkeyPermission: (): Promise<HotkeyPermissionResult> =>
    narrow<HotkeyPermissionResult>(App.RequestHotkeyPermission()),
  openHotkeyPermissionSettings: (): Promise<void> =>
    App.OpenHotkeyPermissionSettings(),
  // recheckHotkeyPermission forces the re-check the window-focus hook does
  // automatically, for a user who granted access and wants an answer now.
  // Re-applies the OS registrations and emits hotkeys:state if it flipped.
  recheckHotkeyPermission: (): Promise<void> =>
    App.RecheckHotkeyPermission(),
  getAudioDevices: (): Promise<AudioDevices> =>
    App.GetAudioDevices(),
  getAudioState: (): Promise<AudioState> => App.GetAudioState(),
  startMicTest: (): Promise<void> => App.StartMicTest(),
  stopMicTest: (): Promise<void> => App.StopMicTest(),
  previewEffect: (id: string): Promise<void> => App.PreviewEffect(id),
  getAudioEffectPresets: (): Promise<AudioEffectPresets> =>
    App.GetAudioEffectPresets(),
  getConnectionState: (): Promise<ConnectionState> =>
    App.GetConnectionState(),
  reconnectVoice: (): Promise<void> => App.ReconnectVoice(),
  getNotifications: (): Promise<NotifySnapshot> =>
    narrow<NotifySnapshot>(App.GetNotifications()),
  markNotificationRead: (id: string): Promise<void> =>
    App.MarkNotificationRead(id),
  markAllNotificationsRead: (): Promise<void> =>
    App.MarkAllNotificationsRead(),
  clearNotifications: (): Promise<void> => App.ClearNotifications(),
  focusMainWindow: (): Promise<void> => App.FocusMainWindow(),
  listProfiles: (): Promise<ProfileSummary[]> =>
    App.ListProfiles(),
  getProfileState: (): Promise<ProfileState> =>
    App.GetProfileState(),
  loadProfile: (path: string): Promise<void> => App.LoadProfile(path),
  saveProfile: (): Promise<void> => App.SaveProfile(),
  saveProfileAs: (name: string, desc: string): Promise<void> =>
    App.SaveProfileAs(name, desc),
  renameProfile: (path: string, name: string, desc: string): Promise<void> =>
    App.RenameProfile(path, name, desc),
  deleteProfile: (path: string): Promise<void> => App.DeleteProfile(path),
  revertProfile: (): Promise<void> => App.RevertProfile(),
  resetLayout: (): Promise<void> => App.ResetLayout(),
  importProfile: (): Promise<void> => App.ImportProfile(),
  exportProfile: (path: string): Promise<void> => App.ExportProfile(path),
  browseProfilesDir: (): Promise<void> => App.BrowseProfilesDir(),
  openProfilesDir: (): Promise<void> => App.OpenProfilesDir(),
  getCommsLayout: (): Promise<Layout> => App.GetCommsLayout(),
  setCommsLayout: (l: Layout): Promise<void> => App.SetCommsLayout(l),
  getHistory: (): Promise<HistoryEntry[]> => App.GetHistory(),
  clearHistory: (): Promise<void> => App.ClearHistory(),
  exportHistoryCsv: (): Promise<void> => App.ExportHistoryCSV(),
};
