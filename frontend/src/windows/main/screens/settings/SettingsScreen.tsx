import { useState } from "react";
import { General } from "./sections/General";
import { Keybinds } from "./sections/Keybinds";
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
      return <Keybinds />;
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
 * (no router this phase).
 *
 * It only READS the shared settings store. Hydrating and subscribing is
 * `useSettingsSync`'s job, mounted once per window shell: doing it here tied
 * the store's liveness to this screen being open, which is why keybind and
 * settings changes never reached the Comms popout (spec DoD 10).
 *
 * General and Keybinds render live controls; the remaining sections render an
 * honest `Deferred` stub until their subsystems land.
 */
export function SettingsScreen() {
  const [section, setSection] = useState("general");

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
