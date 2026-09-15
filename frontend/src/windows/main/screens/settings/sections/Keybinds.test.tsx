import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";

import { Keybinds } from "./Keybinds";
import { useSettings } from "../../../../../shared/store/settings";

const setKeybind = vi.fn();
const beginCapture = vi.fn().mockResolvedValue(undefined);
const endCapture = vi.fn().mockResolvedValue(undefined);

vi.mock("../../../../../shared/api/client", () => ({
  api: {
    setKeybind: (...a: unknown[]) => setKeybind(...a),
    clearKeybind: vi.fn().mockResolvedValue(undefined),
    beginCapture: () => beginCapture(),
    endCapture: () => endCapture(),
  },
}));

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
    beginCapture.mockClear();
    endCapture.mockClear();
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
    await waitFor(() =>
      expect(screen.getByText(/F1 taken from R01 · GUARD \(PTT\)/i)).toBeInTheDocument(),
    );
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
    expect(screen.getByText(/not registerable: key Numpad7 unsupported/i)).toBeInTheDocument();
  });

  it("cancels the previously listening chip when another chip starts capturing", async () => {
    render(<Keybinds />);

    // Start capturing on the unbound global.ptt chip.
    fireEvent.click(screen.getAllByText("—")[0]);
    await waitFor(() => expect(beginCapture).toHaveBeenCalledTimes(1));

    // Before pressing a key, start capturing on a different chip (bound "M").
    fireEvent.click(screen.getByText("M"));
    await waitFor(() => expect(beginCapture).toHaveBeenCalledTimes(2));
    // The first chip's forced cancel (unmount-while-listening) must re-arm
    // the backend for it before the second capture proceeds.
    await waitFor(() => expect(endCapture).toHaveBeenCalledTimes(1));

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
  });
});
