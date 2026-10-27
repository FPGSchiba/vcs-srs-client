import { useState } from "react";
import { General } from "./sections/General";
import { Keybinds } from "./sections/Keybinds";
import { Audio } from "./sections/Audio";
import { Effects } from "./sections/Effects";
import { Deferred } from "./sections/Deferred";
import { Panel } from "../../../../shared/components/Panel";

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

function renderSection(key: string, onNavigate?: (view: string) => void) {
  switch (key) {
    case "general":
      return <General />;
    case "keybinds":
      return <Keybinds />;
    case "audio":
      return <Audio />;
    case "effects":
      return <Effects />;
    case "profiles":
      return (
        <Panel title="PROFILES & LAYOUTS">
          <div className="state-card">
            <div className="state-title">Profiles and layouts are managed on the Profiles screen.</div>
            <button className="btn" onClick={() => onNavigate?.("profiles")}>
              OPEN PROFILES
            </button>
          </div>
        </Panel>
      );
    case "notif":
      return (
        <Deferred
          items={["Alert rules", "Server notifications (needs server support)"]}
          note="Notification sounds: General → Play notification sounds."
        />
      );
    case "misc":
      return <Deferred phase={10} items={["Telemetry", "Crash reports", "Auto-update", "Update channel"]} />;
    case "legacy":
      return (
        <Deferred
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
 * General, Keybinds, Audio and Effects render live controls. Profiles &
 * Layouts points at the Profiles screen through `onNavigate` (inert when the
 * screen is rendered without one). Notifications, Miscellaneous and Legacy
 * render an honest `Deferred` stub until their subsystems land.
 */
export function SettingsScreen({ onNavigate }: { onNavigate?: (view: string) => void } = {}) {
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
      <div style={{ overflow: "auto", padding: 16, minHeight: 0 }}>{renderSection(section, onNavigate)}</div>
    </div>
  );
}
