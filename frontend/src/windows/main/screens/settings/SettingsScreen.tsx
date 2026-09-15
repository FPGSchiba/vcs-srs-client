import { useEffect, useState } from "react";
import { api } from "../../../../shared/api/client";
import { on, EV } from "../../../../shared/api/events";
import { useSettings } from "../../../../shared/store/settings";
import type { Settings, Keybind, HotkeyState } from "../../../../shared/store/settings";
import { General } from "./sections/General";
import { Deferred } from "./sections/Deferred";

interface SettingsSection {
  key: string;
  label: string;
}

const SECTIONS: SettingsSection[] = [
  { key: "general", label: "General" },
  { key: "keybinds", label: "Keybinds" },
  { key: "audio", label: "Audio & Sounds" },
  { key: "effects", label: "Radio Effects" },
  { key: "profiles", label: "Profiles & Layouts" },
  { key: "notif", label: "Notifications" },
  { key: "misc", label: "Miscellaneous" },
  { key: "legacy", label: "Legacy" },
];

function renderSection(key: string) {
  switch (key) {
    case "general":
      return <General />;
    case "keybinds":
      // Task 11 swaps this for the real Keybinds section.
      return (
        <Deferred
          title="KEYBINDS"
          phase={3}
          items={["Global PTT & mute", "Channel hotkeys", "Per-radio bindings", "Quick-status hotkeys"]}
        />
      );
    case "audio":
      return <Deferred phase={4} items={["Device selection", "AGC", "Noise suppression", "VU metering"]} />;
    case "effects":
      return <Deferred phase={4} items={["Radio FX", "Squelch", "Static", "TX/RX tones"]} />;
    case "profiles":
      return <Deferred phase={7} items={["Save/load profiles", "Import & export", "Window layouts"]} />;
    case "notif":
      return <Deferred phase={7} items={["Alert rules", "Sound alerts", "Server notifications"]} />;
    case "misc":
      return <Deferred phase={10} items={["Telemetry", "Crash reports", "Auto-update", "Update channel"]} />;
    case "legacy":
      return (
        <Deferred
          phase={4}
          items={["Legacy SRS audio engine", "Legacy overlay", "Disable hardware acceleration"]}
        />
      );
    default:
      return null;
  }
}

/**
 * SettingsScreen is the Settings screen shell, ported from the design
 * prototype's ScreenSettings: a 200px `.nav-item` rail on the left and a
 * scrolling body on the right, with the active section held in local state
 * (no router this phase). On mount it hydrates the settings store from a
 * one-shot fetch and subscribes to the settings/keybinds/hotkeys events,
 * unsubscribing on unmount so re-opening this screen never accumulates
 * duplicate handlers. Only General renders live controls; the remaining
 * sections render an honest `Deferred` stub until their subsystems land.
 */
export function SettingsScreen() {
  const [section, setSection] = useState("general");

  useEffect(() => {
    api
      .getSettings()
      .then((s) => useSettings.getState().setSettings(s))
      .catch(() => {
        /* not available yet — ignore */
      });
    api
      .getKeybinds()
      .then((k) => useSettings.getState().setKeybinds(k))
      .catch(() => {
        /* not available yet — ignore */
      });
    api
      .getHotkeyState()
      .then((h) => useSettings.getState().setHotkeyState(h))
      .catch(() => {
        /* not available yet — ignore */
      });

    const offs = [
      on<Settings>(EV.settingsChanged, (s) => useSettings.getState().setSettings(s)),
      on<Keybind[]>(EV.keybindsChanged, (k) => useSettings.getState().setKeybinds(k)),
      on<HotkeyState>(EV.hotkeysState, (h) => useSettings.getState().setHotkeyState(h)),
    ];
    return () => offs.forEach((off) => off());
  }, []);

  return (
    <div style={{ height: "100%", display: "grid", gridTemplateColumns: "200px 1fr", minHeight: 0 }}>
      <div style={{ borderRight: "1px solid var(--bd-1)", background: "var(--bg-0)", padding: 10 }}>
        {SECTIONS.map((s) => (
          <div
            key={s.key}
            onClick={() => setSection(s.key)}
            className={`nav-item ${section === s.key ? "active" : ""}`}
            style={{ gridTemplateColumns: "1fr" }}
          >
            <span className="label">{s.label}</span>
          </div>
        ))}
      </div>
      <div style={{ overflow: "auto", padding: 16, minHeight: 0 }}>{renderSection(section)}</div>
    </div>
  );
}
