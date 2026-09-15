import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor, within } from "@testing-library/react";

import { Keybinds } from "./Keybinds";
import { useSettings } from "../../../../../shared/store/settings";

/** The store's untouched default hotkey state, read before any test mutates it. */
const initialHotkeyState = () => useSettings.getInitialState().hotkeys;

const setKeybind = vi.fn();
const beginCapture = vi.fn();
const endCapture = vi.fn().mockResolvedValue(undefined);

vi.mock("../../../../../shared/api/client", () => ({
  api: {
    setKeybind: (...a: unknown[]) => setKeybind(...a),
    clearKeybind: vi.fn().mockResolvedValue(undefined),
    beginCapture: () => beginCapture(),
    endCapture: (token: number) => endCapture(token),
  },
}));

/** Mirrors the backend's capture-token generation counter: every
 * BeginCapture hands out a new, strictly increasing token, and only the
 * newest one is honoured by EndCapture. Starts at 1 so 0 stays reserved for
 * "beginCapture failed". */
let nextToken = 0;

const rows = [
  { action_id: "global.ptt", label: "Global PTT", desc: "Transmits on the Selected radio",
    category: "global", kind: "hold", chord: "" },
  { action_id: "global.mute_toggle", label: "Mute toggle", desc: "",
    category: "global", kind: "press", chord: "M" },
  { action_id: "radio.1.ptt", label: "R01 · GUARD (PTT)", desc: "",
    category: "per_radio", kind: "hold", chord: "F1" },
];

describe("Keybinds section", () => {
  beforeEach(() => {
    setKeybind.mockReset().mockResolvedValue({ stolen: null });
    nextToken = 0;
    beginCapture.mockReset().mockImplementation(() => Promise.resolve(++nextToken));
    endCapture.mockReset().mockResolvedValue(undefined);
    // NOTE: the task-11 brief's literal for `hotkeys` omits `failed`, which
    // `HotkeyState` requires (see shared/store/settings.ts). Completed here
    // rather than weakening the type or reaching for `as any`.
    useSettings.setState({
      settings: null, keybinds: rows, hotkeys: { registered: true, error: "", failed: {} },
    });
  });

  it("groups bindings by category", () => {
    render(<Keybinds />);
    expect(screen.getByText("GLOBAL")).toBeInTheDocument();
    expect(screen.getByText("PER-RADIO BINDINGS")).toBeInTheDocument();
    expect(screen.getByText("Global PTT")).toBeInTheDocument();
    expect(screen.getByText("R01 · GUARD (PTT)")).toBeInTheDocument();
    // Categories with zero rows in this fixture must not render their panel.
    expect(screen.queryByText("CHANNEL HOTKEYS")).not.toBeInTheDocument();
    expect(screen.queryByText("QUICK-STATUS HOTKEYS")).not.toBeInTheDocument();
  });

  it("suspends hotkeys while capturing", async () => {
    render(<Keybinds />);
    fireEvent.click(screen.getAllByText("—")[0]);
    await waitFor(() => expect(beginCapture).toHaveBeenCalled());
  });

  it("sends the capture and re-arms hotkeys", async () => {
    render(<Keybinds />);
    fireEvent.click(screen.getAllByText("—")[0]);
    await waitFor(() => expect(beginCapture).toHaveBeenCalled());
    fireEvent.keyDown(window, { code: "F2", key: "F2" });
    await waitFor(() => {
      expect(setKeybind).toHaveBeenCalledWith("global.ptt", {
        code: "F2", ctrl: false, alt: false, shift: false, super: false,
      });
      expect(endCapture).toHaveBeenCalled();
    });
  });

  it("re-arms hotkeys even when setKeybind rejects", async () => {
    setKeybind.mockRejectedValue(new Error("backend unreachable"));
    render(<Keybinds />);
    fireEvent.click(screen.getAllByText("—")[0]);
    await waitFor(() => expect(beginCapture).toHaveBeenCalled());
    fireEvent.keyDown(window, { code: "F2", key: "F2" });
    await waitFor(() => expect(endCapture).toHaveBeenCalled());
  });

  it("reports which action lost a stolen key", async () => {
    setKeybind.mockResolvedValue({
      stolen: { action_id: "radio.1.ptt", label: "R01 · GUARD (PTT)", chord: "F1" },
    });
    render(<Keybinds />);
    fireEvent.click(screen.getAllByText("—")[0]);
    await waitFor(() => expect(beginCapture).toHaveBeenCalled());
    fireEvent.keyDown(window, { code: "F1", key: "F1" });
    // The warning belongs on the row that just captured the stolen key
    // (global.ptt), not merely somewhere on the page -- scope the query to
    // that row so a regression that renders it under the wrong row (or
    // duplicates it across every row) fails this test.
    const capturingRow = screen.getByText("Global PTT").closest("[data-row]") as HTMLElement;
    await waitFor(() =>
      expect(
        within(capturingRow).getByText(/F1 taken from R01 · GUARD \(PTT\)/i),
      ).toBeInTheDocument(),
    );
    expect(screen.getAllByText(/F1 taken from R01 · GUARD \(PTT\)/i)).toHaveLength(1);
  });

  it("warns when global hotkeys failed to register", () => {
    useSettings.setState({
      settings: null, keybinds: rows,
      hotkeys: { registered: false, error: "permission denied", failed: {} },
    });
    render(<Keybinds />);
    expect(screen.getByText(/global hotkeys unavailable/i)).toBeInTheDocument();
    expect(screen.getByText(/permission denied/i)).toBeInTheDocument();
  });

  it("shows a per-row reason when a saved chord failed to register", () => {
    useSettings.setState({
      settings: null, keybinds: rows,
      hotkeys: {
        registered: true, error: "",
        failed: { "global.mute_toggle": "not registerable: key Numpad7 unsupported" },
      },
    });
    render(<Keybinds />);
    // Scope to the row for global.mute_toggle specifically -- a regression
    // that attached the reason to the wrong row, or to every row, must fail
    // this test, not just "the string exists somewhere".
    const failedRow = screen.getByText("Mute toggle").closest("[data-row]") as HTMLElement;
    expect(
      within(failedRow).getByText(/not registerable: key Numpad7 unsupported/i),
    ).toBeInTheDocument();
    expect(screen.getAllByText(/not registerable: key Numpad7 unsupported/i)).toHaveLength(1);
  });

  it("cancels the previously listening chip when another chip starts capturing", async () => {
    render(<Keybinds />);

    // Start capturing on the unbound global.ptt chip.
    fireEvent.click(screen.getAllByText("—")[0]);
    await waitFor(() => expect(beginCapture).toHaveBeenCalledTimes(1));

    // Before pressing a key, start capturing on a different chip (bound "M").
    fireEvent.click(screen.getByText("M"));
    await waitFor(() => expect(beginCapture).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(endCapture).toHaveBeenCalledTimes(1));

    // The superseded chip's forced cancel (unmount-while-listening) is
    // dispatched AFTER the new chip's beginCapture -- React only unmounts it
    // on commit. It must therefore hand back its OWN, now-stale token (1),
    // never the live one (2): the backend refuses a stale token, which is
    // what keeps every OS hotkey suspended for the capture that is actually
    // running. Ending the live capture here would re-register `M` with the
    // OS and let it swallow the keypress meant to rebind it.
    expect(endCapture).toHaveBeenCalledWith(1);
    expect(endCapture).not.toHaveBeenCalledWith(2);

    // A single keypress must only be attributed to the second (still
    // listening) chip -- if the first chip were still listening too, this
    // would call setKeybind twice, once per chip, off one keypress.
    fireEvent.keyDown(window, { code: "F5", key: "F5" });
    await waitFor(() =>
      expect(setKeybind).toHaveBeenCalledWith("global.mute_toggle", {
        code: "F5", ctrl: false, alt: false, shift: false, super: false,
      }),
    );
    expect(setKeybind).toHaveBeenCalledTimes(1);
    expect(setKeybind).not.toHaveBeenCalledWith("global.ptt", expect.anything());

    // And the live capture, once it completes, ends with its own token.
    await waitFor(() => expect(endCapture).toHaveBeenCalledWith(2));
  });

  it("does not end the live capture when a row switch races an in-flight setKeybind", async () => {
    // The other half of the same hazard: the user presses a key on chip A and
    // then clicks chip B while A's setKeybind is still awaiting. A's `finally`
    // therefore runs AFTER B's beginCapture. It must still surrender only A's
    // own token, or B would capture with every OS hotkey re-armed.
    let resolveSet: (v: unknown) => void = () => {};
    setKeybind.mockImplementation(() => new Promise((res) => { resolveSet = res; }));

    render(<Keybinds />);
    fireEvent.click(screen.getAllByText("—")[0]);
    await waitFor(() => expect(beginCapture).toHaveBeenCalledTimes(1));
    fireEvent.keyDown(window, { code: "F2", key: "F2" });
    await waitFor(() => expect(setKeybind).toHaveBeenCalledTimes(1));
    expect(endCapture).not.toHaveBeenCalled(); // still awaiting setKeybind

    // Start a new capture on another row while A is still in flight.
    fireEvent.click(screen.getByText("M"));
    await waitFor(() => expect(beginCapture).toHaveBeenCalledTimes(2));

    resolveSet({ stolen: null });
    await waitFor(() => expect(endCapture).toHaveBeenCalledTimes(1));
    expect(endCapture).toHaveBeenCalledWith(1);
    expect(endCapture).not.toHaveBeenCalledWith(2);
  });

  it("ends each capture with the token that capture was issued", async () => {
    render(<Keybinds />);
    fireEvent.click(screen.getAllByText("—")[0]);
    await waitFor(() => expect(beginCapture).toHaveBeenCalledTimes(1));
    fireEvent.keyDown(window, { code: "F2", key: "F2" });
    await waitFor(() => expect(endCapture).toHaveBeenCalledWith(1));

    // A second, independent capture gets a fresh token.
    fireEvent.click(screen.getByText("M"));
    await waitFor(() => expect(beginCapture).toHaveBeenCalledTimes(2));
    fireEvent.keyDown(window, { code: "F3", key: "F3" });
    await waitFor(() => expect(endCapture).toHaveBeenCalledWith(2));
  });

  it("does not end a capture that never began", async () => {
    render(<Keybinds />);
    // Escape on a chip the user never clicked cannot happen, but a cancel
    // with no outstanding token must stay a no-op rather than guessing one.
    fireEvent.click(screen.getAllByText("—")[0]);
    await waitFor(() => expect(beginCapture).toHaveBeenCalledTimes(1));
    fireEvent.keyDown(window, { code: "Escape", key: "Escape" });
    await waitFor(() => expect(endCapture).toHaveBeenCalledTimes(1));

    // Re-clicking the now-idle chip twice must not produce a second end for
    // the first capture's token.
    expect(endCapture).toHaveBeenCalledTimes(1);
    expect(endCapture).toHaveBeenCalledWith(1);
  });

  it("does not warn before the backend has reported hotkey health", () => {
    // The store's default: nothing is known to be broken yet. A pessimistic
    // default flashed the banner on every first paint, and stuck if
    // getHotkeyState ever rejected.
    useSettings.setState({ settings: null, keybinds: rows, hotkeys: initialHotkeyState() });
    render(<Keybinds />);
    expect(screen.queryByText(/global hotkeys unavailable/i)).not.toBeInTheDocument();
  });

  it("does not warn when only SOME bindings failed to register", () => {
    // Registered means "no hotkeys are live at all", not "something failed".
    // One unregisterable key must not declare every working binding dead --
    // it gets a per-row reason instead.
    useSettings.setState({
      settings: null, keybinds: rows,
      hotkeys: {
        registered: true, error: "",
        failed: { "global.mute_toggle": "no OS key mapping for Numpad7" },
      },
    });
    render(<Keybinds />);
    expect(screen.queryByText(/global hotkeys unavailable/i)).not.toBeInTheDocument();
    expect(screen.getByText(/no OS key mapping for Numpad7/i)).toBeInTheDocument();
  });
});
