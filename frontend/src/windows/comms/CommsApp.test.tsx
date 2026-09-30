import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, fireEvent, act } from "@testing-library/react";

const getClientState = vi.fn();
const getSettings = vi.fn();
const getKeybinds = vi.fn();
const getHotkeyState = vi.fn();
const getJoystickState = vi.fn();
const getAudioDevices = vi.fn();
const getAudioState = vi.fn();
const getAudioEffectPresets = vi.fn();
const voiceState = vi.fn();
const getCommsLayout = vi.fn().mockResolvedValue({ window: { w: 540, h: 720 }, blocks: [] });
const setCommsLayout = vi.fn();
const revertProfile = vi.fn();
const resetLayout = vi.fn();
const getProfileState = vi.fn().mockResolvedValue({ active_path: "", active_name: "", dirty: false, dir: "" });

vi.mock("../../shared/api/client", () => ({
  api: {
    getClientState: () => getClientState(),
    getSettings: () => getSettings(),
    getKeybinds: () => getKeybinds(),
    getHotkeyState: () => getHotkeyState(),
    getJoystickState: () => getJoystickState(),
    getAudioDevices: () => getAudioDevices(),
    getAudioState: () => getAudioState(),
    getAudioEffectPresets: () => getAudioEffectPresets(),
    voiceState: () => voiceState(),
    getCommsLayout: () => getCommsLayout(),
    setCommsLayout: (l: unknown) => setCommsLayout(l),
    revertProfile: () => revertProfile(),
    resetLayout: () => resetLayout(),
    getProfileState: () => getProfileState(),
    closeWindow: vi.fn(),
    updateRadioInfo: vi.fn(),
    selectRadio: vi.fn(),
  },
}));

const handlers = new Map<string, Set<(d: unknown) => void>>();
const emit = (name: string, data: unknown) => handlers.get(name)?.forEach((h) => h(data));

vi.mock("../../shared/api/events", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../../shared/api/events")>();
  return {
    EV: actual.EV,
    on: (name: string, cb: (d: unknown) => void) => {
      const set = handlers.get(name) ?? new Set();
      set.add(cb);
      handlers.set(name, set);
      return () => set.delete(cb);
    },
  };
});

import { EV } from "../../shared/api/events";
import { useSettings } from "../../shared/store/settings";
import { useRadios } from "../../shared/store/radios";
import { useSession } from "../../shared/store/session";
import { useProfile } from "../../shared/store/profile";
import { CommsApp } from "./CommsApp";

const settings = {
  start_minimized: false, minimize_to_tray: true, show_transmitter_name: true,
  play_connection_sounds: true, play_notification_sounds: true, radio_switch_as_ptt: false,
};

/**
 * Spec DoD 10: "Settings and keybind changes reach the Comms popout without
 * reopening it." The subscriptions used to live in SettingsScreen, a
 * main-window-only component, so this window never observed them at all.
 */
describe("CommsApp settings sync", () => {
  beforeEach(() => {
    handlers.clear();
    getClientState.mockReset().mockResolvedValue({ radios: {}, clients: {}, self: null, self_guid: "" });
    getSettings.mockReset().mockResolvedValue(settings);
    getKeybinds.mockReset().mockResolvedValue([]);
    getHotkeyState.mockReset().mockResolvedValue({ registered: true, error: "", failed: {}, permission: "not_applicable" });
    getJoystickState.mockReset().mockResolvedValue({ supported: false, error: "", devices: [] });
    getAudioDevices.mockReset().mockResolvedValue({ inputs: [], outputs: [] });
    getAudioState.mockReset().mockResolvedValue({
      running: false, input_error: "", output_error: "", overruns: 0, underruns: 0,
    });
    getAudioEffectPresets.mockReset().mockResolvedValue({ voice: [], clipping: [] });
    voiceState.mockReset().mockResolvedValue({ selected_radio: 0, connected: false });
    useSettings.setState({
      settings: null, keybinds: [],
      hotkeys: { registered: true, error: "", failed: {}, permission: "not_applicable" },
      joystick: { supported: false, error: "", devices: [] },
    });
    useRadios.setState({ radios: {}, selectedRadioId: 0, heldPTT: new Set(), globalPttTargetId: 0 });
    useSession.setState({ selfGuid: "" });
  });

  it("hydrates the shared settings store when the popout opens", async () => {
    render(<CommsApp />);
    await waitFor(() => expect(useSettings.getState().settings).toEqual(settings));
  });

  it("applies a settings change made in the main window, without reopening", async () => {
    render(<CommsApp />);
    await waitFor(() => expect(useSettings.getState().settings).not.toBeNull());

    emit(EV.settingsChanged, { ...settings, show_transmitter_name: false });
    expect(useSettings.getState().settings?.show_transmitter_name).toBe(false);
  });

  it("applies a keybind change made in the main window, without reopening", async () => {
    render(<CommsApp />);
    emit(EV.keybindsChanged, [
      {
        action_id: "radio.1.ptt",
        label: "R01 (PTT)",
        desc: "",
        category: "per_radio",
        kind: "hold",
        triggers: [
          { kind: "key", chord: "F7", device: "", device_name: "", label: "F7", connected: true },
        ],
      },
    ]);
    expect(useSettings.getState().keybinds[0].triggers[0].chord).toBe("F7");
  });
});

/**
 * CommsApp used to render `Object.values(radios)[0]` -- the first entry of
 * the WHOLE radios map, which is any client's radios, not necessarily ours.
 * Harmless while nobody else was connected; wrong the moment voice makes
 * multi-client sessions real. It must render `radios[selfGuid]` instead.
 */
describe("CommsApp radio selection", () => {
  beforeEach(() => {
    handlers.clear();
    getSettings.mockReset().mockResolvedValue(settings);
    getKeybinds.mockReset().mockResolvedValue([]);
    getHotkeyState.mockReset().mockResolvedValue({ registered: true, error: "", failed: {}, permission: "not_applicable" });
    getJoystickState.mockReset().mockResolvedValue({ supported: false, error: "", devices: [] });
    getAudioDevices.mockReset().mockResolvedValue({ inputs: [], outputs: [] });
    getAudioState.mockReset().mockResolvedValue({
      running: false, input_error: "", output_error: "", overruns: 0, underruns: 0,
    });
    getAudioEffectPresets.mockReset().mockResolvedValue({ voice: [], clipping: [] });
    voiceState.mockReset().mockResolvedValue({ selected_radio: 0, connected: true });
    useSettings.setState({
      settings: null, keybinds: [],
      hotkeys: { registered: true, error: "", failed: {}, permission: "not_applicable" },
      joystick: { supported: false, error: "", devices: [] },
    });
    useRadios.setState({ radios: {}, selectedRadioId: 0, heldPTT: new Set(), globalPttTargetId: 0 });
    useSession.setState({ selfGuid: "" });
  });

  it("renders the local client's radios, keyed by self_guid, not the first map entry", async () => {
    getClientState.mockReset().mockResolvedValue({
      self_guid: "guid-me",
      self: { name: "Me", coalition: "Red", unit_id: "AB12", role_id: 0 },
      clients: {
        "guid-other": { name: "Other", coalition: "Red", unit_id: "CD34", role_id: 0 },
        "guid-me": { name: "Me", coalition: "Red", unit_id: "AB12", role_id: 0 },
      },
      // "guid-other" sorts first in insertion order -- Object.values(...)[0]
      // would pick its radios, not ours.
      radios: {
        "guid-other": {
          muted: false,
          radios: [{ id: 9, name: "Not Mine", frequency: 251.0, enabled: true, is_intercom: false }],
        },
        "guid-me": {
          muted: false,
          radios: [{ id: 1, name: "Mine", frequency: 118.5, enabled: true, is_intercom: false }],
        },
      },
    });

    render(<CommsApp />);

    expect(await screen.findByDisplayValue("Mine")).toBeInTheDocument();
    expect(screen.queryByDisplayValue("Not Mine")).not.toBeInTheDocument();
  });

  it("shows the empty state when the local client has no radios yet", async () => {
    getClientState.mockReset().mockResolvedValue({
      self_guid: "guid-me",
      self: { name: "Me", coalition: "Red", unit_id: "AB12", role_id: 0 },
      clients: { "guid-me": { name: "Me", coalition: "Red", unit_id: "AB12", role_id: 0 } },
      radios: {},
    });

    render(<CommsApp />);

    expect(await screen.findByText(/no radios/i)).toBeInTheDocument();
  });
});

type PState = { active_path: string; active_name: string; dirty: boolean; dir: string };

/** Seeds both the store and the backend snapshot CommsApp hydrates from. */
function setProfile(state: PState) {
  useProfile.setState({ state, profiles: [] });
  getProfileState.mockReset().mockResolvedValue(state);
}

function setupBackend() {
  handlers.clear();
  getClientState.mockReset().mockResolvedValue({ radios: {}, clients: {}, self: null, self_guid: "" });
  getSettings.mockReset().mockResolvedValue(settings);
  getKeybinds.mockReset().mockResolvedValue([]);
  getHotkeyState.mockReset().mockResolvedValue({ registered: true, error: "", failed: {}, permission: "not_applicable" });
  getJoystickState.mockReset().mockResolvedValue({ supported: false, error: "", devices: [] });
  getAudioDevices.mockReset().mockResolvedValue({ inputs: [], outputs: [] });
  getAudioState.mockReset().mockResolvedValue({
    running: false, input_error: "", output_error: "", overruns: 0, underruns: 0,
  });
  getAudioEffectPresets.mockReset().mockResolvedValue({ voice: [], clipping: [] });
  voiceState.mockReset().mockResolvedValue({ selected_radio: 0, connected: true });
  getCommsLayout.mockReset().mockResolvedValue({ window: { w: 540, h: 720 }, blocks: [] });
  setCommsLayout.mockReset().mockResolvedValue(undefined);
  revertProfile.mockReset().mockResolvedValue(undefined);
  resetLayout.mockReset().mockResolvedValue(undefined);
  useSettings.setState({
    settings: null, keybinds: [],
    hotkeys: { registered: true, error: "", failed: {}, permission: "not_applicable" },
    joystick: { supported: false, error: "", devices: [] },
  });
  useRadios.setState({ radios: {}, selectedRadioId: 0, heldPTT: new Set(), globalPttTargetId: 0 });
  useSession.setState({ selfGuid: "" });
  setProfile({ active_path: "", active_name: "", dirty: false, dir: "/p" });
}

describe("CommsApp profile chrome", () => {
  beforeEach(setupBackend);

  it("shows the dirty dot and REVERT only when dirty with an active profile", async () => {
    setProfile({ active_path: "/p/x.vcs.json", active_name: "Fleet Op", dirty: true, dir: "/p" });
    render(<CommsApp />);
    expect(await screen.findByTitle(/unsaved/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /revert/i })).toBeInTheDocument();
  });

  it("hides REVERT when clean", async () => {
    setProfile({ active_path: "/p/x.vcs.json", active_name: "Fleet Op", dirty: false, dir: "/p" });
    render(<CommsApp />);
    await screen.findByText("Communications");
    expect(await screen.findByText("Fleet Op")).toBeInTheDocument();
    expect(screen.queryByTitle(/unsaved/i)).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /revert/i })).not.toBeInTheDocument();
  });

  it("hides REVERT when no profile is active, but still offers RESET", async () => {
    setProfile({ active_path: "", active_name: "", dirty: false, dir: "/p" });
    render(<CommsApp />);
    await screen.findByText("Communications");
    expect(screen.queryByRole("button", { name: /revert/i })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /reset/i })).toBeInTheDocument();
  });

  it("shows the dirty dot but no REVERT when dirty without an active profile", async () => {
    setProfile({ active_path: "", active_name: "", dirty: true, dir: "/p" });
    render(<CommsApp />);
    expect(await screen.findByTitle(/unsaved/i)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /revert/i })).not.toBeInTheDocument();
  });

  it("follows profile:state events from the backend", async () => {
    setProfile({ active_path: "/p/x.vcs.json", active_name: "Fleet Op", dirty: false, dir: "/p" });
    render(<CommsApp />);
    await screen.findByText("Fleet Op");
    expect(screen.queryByTitle(/unsaved/i)).not.toBeInTheDocument();
    act(() => emit(EV.profileState, { active_path: "/p/x.vcs.json", active_name: "Fleet Op", dirty: true, dir: "/p" }));
    expect(screen.getByTitle(/unsaved/i)).toBeInTheDocument();
  });

  it("calls the backend when REVERT and RESET are clicked", async () => {
    setProfile({ active_path: "/p/x.vcs.json", active_name: "Fleet Op", dirty: true, dir: "/p" });
    render(<CommsApp />);
    fireEvent.click(await screen.findByRole("button", { name: /revert/i }));
    expect(revertProfile).toHaveBeenCalledTimes(1);
    expect(resetLayout).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: /reset/i }));
    expect(resetLayout).toHaveBeenCalledTimes(1);
    expect(revertProfile).toHaveBeenCalledTimes(1);
  });
});

const twoRadios = {
  self_guid: "guid-me",
  self: { name: "Me", coalition: "Red", unit_id: "AB12", role_id: 0 },
  clients: { "guid-me": { name: "Me", coalition: "Red", unit_id: "AB12", role_id: 0 } },
  radios: {
    "guid-me": {
      muted: false,
      radios: [
        { id: 1, name: "Alpha", frequency: 118.5, enabled: true, is_intercom: false },
        { id: 2, name: "Bravo", frequency: 251.0, enabled: true, is_intercom: false },
      ],
    },
  },
};

/** The sized RadioBlock wrappers, in DOM order. */
function blockEls() {
  return screen.getAllByRole("option").map((o) => o.parentElement!.parentElement as HTMLElement);
}

/** The radio name input inside a block. */
function nameIn(block: HTMLElement): string {
  return (block.querySelector('input[aria-label="radio name"]') as HTMLInputElement).value;
}

describe("CommsApp layout grid", () => {
  beforeEach(() => {
    setupBackend();
    getClientState.mockReset().mockResolvedValue(twoRadios);
    getCommsLayout.mockReset().mockResolvedValue({
      window: { w: 600, h: 800 },
      blocks: [
        { radio_id: 2, w: 300, h: 120 },
        { radio_id: 1, w: 400, h: 150 },
      ],
    });
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("renders blocks in layout order at their stored sizes, in a wrapping flow", async () => {
    render(<CommsApp />);
    await screen.findByDisplayValue("Alpha");
    await waitFor(() => expect(blockEls()[0].style.width).toBe("300px"));
    const els = blockEls();
    expect(els[0].style.height).toBe("120px");
    expect(nameIn(els[0])).toBe("Bravo");
    expect(els[1].style.width).toBe("400px");
    expect(els[1].style.height).toBe("150px");
    const flow = els[0].parentElement as HTMLElement;
    expect(flow.style.flexWrap).toBe("wrap");
    expect(flow.style.padding).toBe("12px");
    expect(flow.style.gap).toBe("8px");
    expect(els[0].style.flexShrink).toBe("0");
  });

  it("gives a radio with no stored block the default size", async () => {
    getCommsLayout.mockReset().mockResolvedValue({
      window: { w: 600, h: 800 },
      blocks: [{ radio_id: 1, w: 400, h: 150 }],
    });
    render(<CommsApp />);
    await screen.findByDisplayValue("Bravo");
    await waitFor(() => expect(blockEls()[0].style.width).toBe("400px"));
    expect(blockEls()[1].style.width).toBe("516px");
    expect(blockEls()[1].style.height).toBe("180px");
    // Hydration alone is not a user edit: nothing may be written back, even
    // once the debounce window has long passed.
    await act(async () => { await new Promise((r) => setTimeout(r, 400)); });
    expect(setCommsLayout).not.toHaveBeenCalled();
  });

  it("debounces resize into ONE setCommsLayout carrying the whole layout", async () => {
    render(<CommsApp />);
    await screen.findByDisplayValue("Alpha");
    await waitFor(() => expect(blockEls()[0].style.width).toBe("300px"));
    vi.useFakeTimers();
    const handles = screen.getAllByRole("button", { name: /resize/i });
    fireEvent.keyDown(handles[0], { key: "ArrowRight" });
    fireEvent.keyDown(handles[0], { key: "ArrowRight" });
    // Local state follows immediately...
    expect(blockEls()[0].style.width).toBe("332px");
    // ...the IPC call does not.
    expect(setCommsLayout).not.toHaveBeenCalled();
    act(() => { vi.advanceTimersByTime(299); });
    expect(setCommsLayout).not.toHaveBeenCalled();
    act(() => { vi.advanceTimersByTime(2); });
    expect(setCommsLayout).toHaveBeenCalledTimes(1);
    expect(setCommsLayout).toHaveBeenCalledWith({
      window: { w: 600, h: 800 },
      blocks: [
        { radio_id: 2, w: 332, h: 120 },
        { radio_id: 1, w: 400, h: 150 },
      ],
    });
  });

  it("reorders blocks and saves the new order", async () => {
    render(<CommsApp />);
    await screen.findByDisplayValue("Alpha");
    await waitFor(() => expect(blockEls()[0].style.width).toBe("300px"));
    vi.useFakeTimers();
    const store: Record<string, string> = { "text/plain": "0" };
    const dataTransfer = {
      setData: (k: string, v: string) => { store[k] = v; },
      getData: (k: string) => store[k] ?? "",
      types: ["text/plain"],
    };
    fireEvent.drop(blockEls()[1], { dataTransfer });
    expect(nameIn(blockEls()[0])).toBe("Alpha");
    act(() => { vi.advanceTimersByTime(301); });
    expect(setCommsLayout).toHaveBeenCalledWith({
      window: { w: 600, h: 800 },
      blocks: [
        { radio_id: 1, w: 400, h: 150 },
        { radio_id: 2, w: 300, h: 120 },
      ],
    });
  });

  it("keeps the stored blocks of radios that are not present when it saves", async () => {
    getCommsLayout.mockReset().mockResolvedValue({
      window: { w: 600, h: 800 },
      blocks: [
        { radio_id: 7, w: 333, h: 111 },
        { radio_id: 1, w: 400, h: 150 },
        { radio_id: 2, w: 300, h: 120 },
      ],
    });
    render(<CommsApp />);
    await screen.findByDisplayValue("Alpha");
    await waitFor(() => expect(blockEls()[0].style.width).toBe("400px"));
    vi.useFakeTimers();
    fireEvent.keyDown(screen.getAllByRole("button", { name: /resize/i })[0], { key: "ArrowRight" });
    act(() => { vi.advanceTimersByTime(301); });
    expect(setCommsLayout).toHaveBeenCalledWith({
      window: { w: 600, h: 800 },
      blocks: [
        { radio_id: 1, w: 416, h: 150 },
        { radio_id: 2, w: 300, h: 120 },
        { radio_id: 7, w: 333, h: 111 },
      ],
    });
  });

  it("flushes a pending layout on unmount instead of dropping it", async () => {
    const { unmount } = render(<CommsApp />);
    await screen.findByDisplayValue("Alpha");
    await waitFor(() => expect(blockEls()[0].style.width).toBe("300px"));
    fireEvent.keyDown(screen.getAllByRole("button", { name: /resize/i })[0], { key: "ArrowRight" });
    expect(setCommsLayout).not.toHaveBeenCalled();
    unmount();
    expect(setCommsLayout).toHaveBeenCalledTimes(1);
  });

  it("re-reads the layout when the backend changes it (revert/reset/profile load)", async () => {
    render(<CommsApp />);
    await screen.findByDisplayValue("Alpha");
    await waitFor(() => expect(blockEls()[0].style.width).toBe("300px"));
    getCommsLayout.mockReset().mockResolvedValue({
      window: { w: 600, h: 800 },
      blocks: [
        { radio_id: 1, w: 500, h: 200 },
        { radio_id: 2, w: 260, h: 100 },
      ],
    });
    act(() => emit(EV.profileState, { active_path: "", active_name: "", dirty: false, dir: "/p" }));
    await waitFor(() => expect(blockEls()[0].style.width).toBe("500px"));
    expect(nameIn(blockEls()[0])).toBe("Alpha");
    expect(blockEls()[1].style.height).toBe("100px");
  });

  it("does not let a profile:state echo clobber an edit that is still pending", async () => {
    render(<CommsApp />);
    await screen.findByDisplayValue("Alpha");
    await waitFor(() => expect(blockEls()[0].style.width).toBe("300px"));
    fireEvent.keyDown(screen.getAllByRole("button", { name: /resize/i })[0], { key: "ArrowRight" });
    // The backend has not seen the edit yet, so a re-read returns the old layout.
    act(() => emit(EV.profileState, { active_path: "", active_name: "", dirty: true, dir: "/p" }));
    await act(async () => { await Promise.resolve(); await Promise.resolve(); });
    expect(blockEls()[0].style.width).toBe("316px");
  });
});
