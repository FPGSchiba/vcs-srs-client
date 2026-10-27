import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, waitFor } from "@testing-library/react";

const getClientState = vi.fn();
const voiceState = vi.fn();

vi.mock("../api/client", () => ({
  api: {
    getClientState: () => getClientState(),
    voiceState: () => voiceState(),
  },
}));

const handlers = new Map<string, Set<(d: unknown) => void>>();
const emit = (name: string, data: unknown) => handlers.get(name)?.forEach((h) => h(data));

vi.mock("../api/events", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../api/events")>();
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

import { EV } from "../api/events";
import { useClients } from "./clients";
import { useRadios } from "./radios";
import { useSession } from "./session";
import { useClientStateSync } from "./useClientStateSync";

function Harness() {
  useClientStateSync();
  return null;
}

const me = { name: "Me", coalition: "Red", unit_id: "AB12", role_id: 0 };
const radioEntry = (name: string) => ({
  muted: false,
  radios: [{ id: 1, name, frequency: 118.5, enabled: true, is_intercom: false }],
});
const snapFor = (guid: string, radioName: string) => ({
  self_guid: guid,
  self: me,
  clients: { [guid]: me },
  radios: { [guid]: radioEntry(radioName) },
});

const tick = () => new Promise<void>((r) => setTimeout(r, 0));

describe("useClientStateSync", () => {
  beforeEach(() => {
    handlers.clear();
    getClientState.mockReset().mockResolvedValue(snapFor("guid-a", "Alpha"));
    voiceState.mockReset().mockResolvedValue({ selected_radio: 0, connected: true });
    useClients.setState({ clients: {} });
    useRadios.setState({ radios: {}, selectedRadioId: 0, heldPTT: new Set(), globalPttTargetId: 0 });
    useSession.setState({ self: null, selfGuid: "" });
  });

  it("hydrates self and selfGuid", async () => {
    render(<Harness />);
    await waitFor(() => expect(useSession.getState().selfGuid).toBe("guid-a"));
    expect(useSession.getState().self).toEqual({ callsign: "Me", ffid: "AB12", coalition: "Red" });
    expect(useClients.getState().clients["guid-a"]).toEqual(me);
    expect(useRadios.getState().radios["guid-a"].radios[0].name).toBe("Alpha");
  });

  it("connected re-hydrates", async () => {
    render(<Harness />);
    await waitFor(() => expect(useSession.getState().selfGuid).toBe("guid-a"));
    getClientState.mockResolvedValue(snapFor("guid-b", "Bravo"));
    voiceState.mockResolvedValue({ selected_radio: 4, connected: true });
    emit(EV.controlConnection, "connected");
    await waitFor(() => expect(useSession.getState().selfGuid).toBe("guid-b"));
    await waitFor(() => expect(useRadios.getState().selectedRadioId).toBe(4));
    expect(getClientState).toHaveBeenCalledTimes(2);
  });

  it("logged_out clears radios, selfGuid, selection and held PTT", async () => {
    voiceState.mockResolvedValue({ selected_radio: 2, connected: true });
    render(<Harness />);
    await waitFor(() => expect(useSession.getState().selfGuid).toBe("guid-a"));
    await waitFor(() => expect(useRadios.getState().selectedRadioId).toBe(2));
    useRadios.getState().setPTTHeld("global.ptt", true);
    expect(useRadios.getState().globalPttTargetId).toBe(2);

    emit(EV.authSession, "logged_out");

    expect(useRadios.getState().radios).toEqual({});
    expect(useRadios.getState().selectedRadioId).toBe(0);
    expect(useRadios.getState().heldPTT.size).toBe(0);
    expect(useRadios.getState().globalPttTargetId).toBe(0);
    expect(useClients.getState().clients).toEqual({});
    expect(useSession.getState().self).toBeNull();
    expect(useSession.getState().selfGuid).toBe("");
  });

  it("radio.3.select hotkey selects 3; radio.3.ptt does not change selection", async () => {
    render(<Harness />);
    await waitFor(() => expect(voiceState).toHaveBeenCalled());
    await tick();
    emit(EV.hotkeyPressed, { action_id: "radio.3.select" });
    expect(useRadios.getState().selectedRadioId).toBe(3);
    emit(EV.hotkeyPressed, { action_id: "radio.5.ptt" });
    expect(useRadios.getState().selectedRadioId).toBe(3);
  });

  it("a radio.<n>.select hotkey during the voiceState hydrate is not overwritten by it", async () => {
    let resolveVoice!: (v: unknown) => void;
    voiceState.mockReset().mockReturnValueOnce(new Promise((r) => (resolveVoice = r)));
    render(<Harness />);
    await waitFor(() => expect(voiceState).toHaveBeenCalledTimes(1));
    emit(EV.hotkeyPressed, { action_id: "radio.3.select" });
    expect(useRadios.getState().selectedRadioId).toBe(3);
    resolveVoice({ selected_radio: 1, connected: true });
    await tick();
    await tick();
    expect(useRadios.getState().selectedRadioId).toBe(3);
  });

  it("profile:state re-reads voiceState", async () => {
    render(<Harness />);
    await waitFor(() => expect(voiceState).toHaveBeenCalledTimes(1));
    voiceState.mockResolvedValue({ selected_radio: 6, connected: true });
    emit(EV.profileState, { active_path: "", active_name: "", dirty: false, dir: "" });
    await waitFor(() => expect(useRadios.getState().selectedRadioId).toBe(6));
    expect(voiceState).toHaveBeenCalledTimes(2);
  });

  it("a radio_update during the client-state hydrate triggers a re-fetch", async () => {
    let resolveFirst!: (v: unknown) => void;
    getClientState
      .mockReset()
      .mockReturnValueOnce(new Promise((r) => (resolveFirst = r)))
      .mockResolvedValueOnce(snapFor("guid-a", "Fresh"));
    render(<Harness />);
    emit(EV.radioUpdate, { guid: "guid-a", radio: radioEntry("Fresh") });
    resolveFirst(snapFor("guid-a", "Stale"));
    await waitFor(() => expect(getClientState).toHaveBeenCalledTimes(2));
    await tick();
    expect(useRadios.getState().radios["guid-a"].radios[0].name).toBe("Fresh");
  });
});
