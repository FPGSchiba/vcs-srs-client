import { describe, it, expect, beforeEach } from "vitest";
import { useSettings } from "./settings";

const blank = {
  start_minimized: false, minimize_to_tray: true, show_transmitter_name: true,
  play_connection_sounds: true, radio_switch_as_ptt: false,
};

describe("settings store", () => {
  beforeEach(() => {
    useSettings.setState({ settings: null, keybinds: [], hotkeys: { registered: false, error: "" } });
  });

  it("starts empty", () => {
    expect(useSettings.getState().settings).toBeNull();
    expect(useSettings.getState().keybinds).toEqual([]);
  });

  it("stores settings", () => {
    useSettings.getState().setSettings(blank);
    expect(useSettings.getState().settings?.minimize_to_tray).toBe(true);
  });

  it("replaces the whole keybind list rather than merging", () => {
    useSettings.getState().setKeybinds([
      { action_id: "a", label: "A", desc: "", category: "global", kind: "press", chord: "F1" },
      { action_id: "b", label: "B", desc: "", category: "global", kind: "press", chord: "F2" },
    ]);
    useSettings.getState().setKeybinds([
      { action_id: "a", label: "A", desc: "", category: "global", kind: "press", chord: "F3" },
    ]);
    const kb = useSettings.getState().keybinds;
    expect(kb).toHaveLength(1);
    expect(kb[0].chord).toBe("F3");
  });

  it("stores hotkey registration state", () => {
    useSettings.getState().setHotkeyState({ registered: false, error: "permission denied" });
    expect(useSettings.getState().hotkeys.error).toBe("permission denied");
  });
});
