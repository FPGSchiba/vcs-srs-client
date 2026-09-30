import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, fireEvent, act, within } from "@testing-library/react";

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
const saveProfile = vi.fn();
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
    saveProfile: () => saveProfile(),
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
import { VARIANTS } from "./variants";
import { GRID_PAD } from "../../shared/layout";

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

    expect(await screen.findByText("Mine")).toBeInTheDocument();
    expect(screen.queryByText("Not Mine")).not.toBeInTheDocument();
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
  saveProfile.mockReset().mockResolvedValue(undefined);
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

  it("offers SAVE only when dirty with an active profile", async () => {
    setProfile({ active_path: "", active_name: "", dirty: true, dir: "/p" });
    const { unmount } = render(<CommsApp />);
    await screen.findByTitle(/unsaved/i);
    expect(screen.queryByRole("button", { name: /^save$/i })).not.toBeInTheDocument();
    unmount();

    setProfile({ active_path: "/p/x.vcs.json", active_name: "Fleet Op", dirty: false, dir: "/p" });
    const second = render(<CommsApp />);
    await screen.findByText("Fleet Op");
    expect(screen.queryByRole("button", { name: /^save$/i })).not.toBeInTheDocument();
    second.unmount();

    setProfile({ active_path: "/p/x.vcs.json", active_name: "Fleet Op", dirty: true, dir: "/p" });
    render(<CommsApp />);
    expect(await screen.findByRole("button", { name: /^save$/i })).toBeInTheDocument();
  });

  it("SAVE calls saveProfile and the dirty state clears on the resulting profile:state", async () => {
    setProfile({ active_path: "/p/x.vcs.json", active_name: "Fleet Op", dirty: true, dir: "/p" });
    saveProfile.mockImplementation(async () => {
      emit(EV.profileState, { active_path: "/p/x.vcs.json", active_name: "Fleet Op", dirty: false, dir: "/p" });
    });
    render(<CommsApp />);
    fireEvent.click(await screen.findByRole("button", { name: /^save$/i }));
    expect(saveProfile).toHaveBeenCalledTimes(1);
    expect(revertProfile).not.toHaveBeenCalled();
    await waitFor(() => expect(screen.queryByTitle(/unsaved/i)).not.toBeInTheDocument());
    expect(screen.queryByRole("button", { name: /^save$/i })).not.toBeInTheDocument();
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
  return screen.getAllByTestId("radio-block");
}

/** The radio name label inside a block (a label now, not a permanent input). */
function nameIn(block: HTMLElement): string {
  return block.querySelector('[title^="Double-click"]')!.textContent ?? "";
}

describe("CommsApp layout grid", () => {
  beforeEach(() => {
    setupBackend();
    getClientState.mockReset().mockResolvedValue(twoRadios);
    getCommsLayout.mockReset().mockResolvedValue({
      window: { w: 600, h: 800 },
      blocks: [
        { radio_id: 2, variant: "narrow-v" },
        { radio_id: 1, variant: "vertical" },
      ],
    });
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("renders blocks in layout order at their variants' sizes, in a wrapping flow", async () => {
    render(<CommsApp />);
    await screen.findByText("Alpha");
    await waitFor(() => expect(blockEls()[0].style.width).toBe("150px"));
    const els = blockEls();
    expect(els[0].style.height).toBe("136px");
    expect(nameIn(els[0])).toBe("Bravo");
    expect(els[1].style.width).toBe("280px");
    expect(els[1].style.height).toBe("168px");
    const flow = screen.getByRole("list", { name: "Radios" });
    expect(flow.style.flexWrap).toBe("wrap");
    expect(flow.style.padding).toBe("12px");
    expect(flow.style.gap).toBe("8px");
    expect(els[0].style.flexShrink).toBe("0");
  });

  it("gives a radio with no stored block the default variant", async () => {
    getCommsLayout.mockReset().mockResolvedValue({
      window: { w: 600, h: 800 },
      blocks: [{ radio_id: 1, variant: "narrow-v" }],
    });
    render(<CommsApp />);
    await screen.findByText("Bravo");
    await waitFor(() => expect(blockEls()[0].style.width).toBe("150px"));
    expect(blockEls()[1].style.width).toBe("280px");
    expect(blockEls()[1].style.height).toBe("168px");
    // Hydration alone is not a user edit: nothing may be written back, even
    // once the debounce window has long passed.
    await act(async () => { await new Promise((r) => setTimeout(r, 400)); });
    expect(setCommsLayout).not.toHaveBeenCalled();
  });

  it("renders a stored variant rather than the default", async () => {
    render(<CommsApp />);
    await screen.findByText("Alpha");
    await waitFor(() => expect(blockEls()[0].style.width).toBe("150px"));
    expect(blockEls()[0].style.height).toBe("136px");
  });

  it("keeps every variant inside the popout's content width", () => {
    // GRID_PAD is 12 a side, so a 540-wide popout has 516 of content. No
    // variant may be wider, or the card sits flush against the clip edge and
    // its border is cut -- this was bug 2.
    for (const v of VARIANTS) expect(v.w).toBeLessThanOrEqual(540 - 2 * GRID_PAD);
  });

  it("persists a resize as a variant id, debounced into ONE setCommsLayout", async () => {
    render(<CommsApp />);
    await screen.findByText("Alpha");
    await waitFor(() => expect(blockEls()[0].style.width).toBe("150px"));
    vi.useFakeTimers();
    const handles = screen.getAllByRole("button", { name: /resize/i });
    fireEvent.keyDown(handles[1], { key: "ArrowLeft" });
    // Local state follows immediately...
    expect(blockEls()[1].style.width).not.toBe("280px");
    // ...the IPC call does not.
    expect(setCommsLayout).not.toHaveBeenCalled();
    act(() => { vi.advanceTimersByTime(299); });
    expect(setCommsLayout).not.toHaveBeenCalled();
    act(() => { vi.advanceTimersByTime(2); });
    expect(setCommsLayout).toHaveBeenCalledTimes(1);
    const sent = setCommsLayout.mock.calls.at(-1)![0];
    expect(sent.window).toEqual({ w: 600, h: 800 });
    expect(sent.blocks[0]).toEqual({ radio_id: 2, variant: "narrow-v" });
    expect(sent.blocks[1]).toMatchObject({ radio_id: 1, variant: expect.any(String) });
    expect(sent.blocks[1].variant).not.toBe("vertical");
    for (const b of sent.blocks) {
      expect(b).not.toHaveProperty("w");
      expect(b).not.toHaveProperty("h");
    }
  });

  it("reorders blocks and saves the new order", async () => {
    render(<CommsApp />);
    await screen.findByText("Alpha");
    await waitFor(() => expect(blockEls()[0].style.width).toBe("150px"));
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
        { radio_id: 1, variant: "vertical" },
        { radio_id: 2, variant: "narrow-v" },
      ],
    });
  });

  it("keeps the stored blocks of radios that are not present when it saves", async () => {
    getCommsLayout.mockReset().mockResolvedValue({
      window: { w: 600, h: 800 },
      blocks: [
        { radio_id: 7, variant: "horizontal" },
        { radio_id: 1, variant: "vertical" },
        { radio_id: 2, variant: "narrow-v" },
      ],
    });
    render(<CommsApp />);
    await screen.findByText("Alpha");
    await waitFor(() => expect(blockEls()[0].style.width).toBe("280px"));
    vi.useFakeTimers();
    fireEvent.keyDown(screen.getAllByRole("button", { name: /resize/i })[0], { key: "ArrowLeft" });
    act(() => { vi.advanceTimersByTime(301); });
    const sent = setCommsLayout.mock.calls.at(-1)![0];
    expect(sent.blocks.map((b: { radio_id: number }) => b.radio_id)).toEqual([1, 2, 7]);
    expect(sent.blocks[0].variant).not.toBe("vertical");
    expect(sent.blocks[2]).toEqual({ radio_id: 7, variant: "horizontal" });
  });

  it("flushes a pending layout on unmount instead of dropping it", async () => {
    const { unmount } = render(<CommsApp />);
    await screen.findByText("Alpha");
    await waitFor(() => expect(blockEls()[0].style.width).toBe("150px"));
    fireEvent.keyDown(screen.getAllByRole("button", { name: /resize/i })[1], { key: "ArrowLeft" });
    expect(setCommsLayout).not.toHaveBeenCalled();
    unmount();
    expect(setCommsLayout).toHaveBeenCalledTimes(1);
  });

  it("re-reads the layout when the backend changes it (revert/reset/profile load)", async () => {
    render(<CommsApp />);
    await screen.findByText("Alpha");
    await waitFor(() => expect(blockEls()[0].style.width).toBe("150px"));
    getCommsLayout.mockReset().mockResolvedValue({
      window: { w: 600, h: 800 },
      blocks: [
        { radio_id: 1, variant: "horizontal" },
        { radio_id: 2, variant: "vertical" },
      ],
    });
    act(() => emit(EV.profileState, { active_path: "", active_name: "", dirty: false, dir: "/p" }));
    await waitFor(() => expect(blockEls()[0].style.width).toBe("360px"));
    expect(nameIn(blockEls()[0])).toBe("Alpha");
    expect(blockEls()[1].style.height).toBe("168px");
  });

  it("does not let a profile:state echo clobber an edit that is still pending", async () => {
    render(<CommsApp />);
    await screen.findByText("Alpha");
    await waitFor(() => expect(blockEls()[0].style.width).toBe("150px"));
    fireEvent.keyDown(screen.getAllByRole("button", { name: /resize/i })[1], { key: "ArrowLeft" });
    const edited = blockEls()[1].style.width;
    expect(edited).not.toBe("280px");
    // The backend has not seen the edit yet, so a re-read returns the old layout.
    act(() => emit(EV.profileState, { active_path: "", active_name: "", dirty: true, dir: "/p" }));
    await act(async () => { await Promise.resolve(); await Promise.resolve(); });
    expect(blockEls()[1].style.width).toBe(edited);
  });
});

describe("CommsApp chrome", () => {
  beforeEach(() => {
    setupBackend();
    getClientState.mockReset().mockResolvedValue(twoRadios);
    setProfile({ active_path: "/p/x.vcs.json", active_name: "Fleet Op", dirty: true, dir: "/p" });
  });

  it("gives the three-column grid exactly three children", async () => {
    render(<CommsApp />);
    await screen.findByTitle(/unsaved/i);
    const chrome = document.querySelector(".popout-chrome")!;
    expect(chrome.children).toHaveLength(3);
    const lead = chrome.firstElementChild as HTMLElement;
    const ctrl = chrome.lastElementChild as HTMLElement;
    // Leading group carries the title and the dirty dot.
    expect(lead).toHaveTextContent("Communications");
    expect(lead.querySelector('[title="Unsaved layout changes"]')).not.toBeNull();
    // The controls group carries all four buttons.
    expect(ctrl).toHaveClass("ctrl");
    for (const label of ["SAVE", "REVERT", "RESET", "close"]) {
      expect(within(ctrl).getByRole("button", { name: new RegExp(`^${label}$`, "i") })).toBeInTheDocument();
    }
    // The .btn width exemption must not be able to reach the close button.
    expect(within(ctrl).getByRole("button", { name: /close/i }).className).not.toMatch(/btn/);
  });

  it("keeps every control present in the chrome", async () => {
    render(<CommsApp />);
    await screen.findByTitle(/unsaved/i);
    for (const label of ["SAVE", "REVERT", "RESET"]) {
      expect(screen.getByRole("button", { name: label })).toBeInTheDocument();
    }
    expect(screen.getByRole("button", { name: /close/i })).toBeInTheDocument();
  });
});
