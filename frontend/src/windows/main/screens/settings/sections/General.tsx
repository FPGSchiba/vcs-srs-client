import { Panel } from "../../../../../shared/components/Panel";
import { SettingRow } from "../../../../../shared/components/SettingRow";
import { Toggle } from "../../../../../shared/components/Toggle";
import { api } from "../../../../../shared/api/client";
import { useSettings } from "../../../../../shared/store/settings";
import type { Settings } from "../../../../../shared/store/settings";

/**
 * General renders the five General-section toggles from the design
 * prototype's SettingsGeneral. Go is the single source of truth: every
 * toggle calls `api.setSettings` with the full struct and never updates the
 * store itself — the row only re-renders once the backend's
 * `settings:changed` event lands and SettingsScreen's subscription writes
 * the new struct into the store. There is no local mirror state here.
 */
export function General() {
  const settings = useSettings((s) => s.settings);

  if (!settings) return null;

  const update = (patch: Partial<Settings>) => {
    void api.setSettings({ ...settings, ...patch });
  };

  return (
    <Panel title="GENERAL">
      <SettingRow
        label="Start minimized"
        desc="Launch VCS as an icon in the system tray."
        control={
          <Toggle
            on={settings.start_minimized}
            onChange={(v) => update({ start_minimized: v })}
            lg
            aria-label="Start minimized"
          />
        }
      />
      <SettingRow
        label="Minimize to tray"
        desc="Closing the window keeps VCS running in tray."
        control={
          <Toggle
            on={settings.minimize_to_tray}
            onChange={(v) => update({ minimize_to_tray: v })}
            lg
            aria-label="Minimize to tray"
          />
        }
      />
      <SettingRow
        label="Show transmitter name"
        desc="Display caller's callsign on incoming transmissions."
        control={
          <Toggle
            on={settings.show_transmitter_name}
            onChange={(v) => update({ show_transmitter_name: v })}
            lg
            aria-label="Show transmitter name"
          />
        }
      />
      <SettingRow
        label="Play connection sounds"
        desc="SFX on connect / disconnect / reconnect."
        control={
          <Toggle
            on={settings.play_connection_sounds}
            onChange={(v) => update({ play_connection_sounds: v })}
            lg
            aria-label="Play connection sounds"
          />
        }
      />
      <SettingRow
        label="Radio switch as PTT"
        desc="Hardware radio mode-switch acts as push-to-talk while held."
        control={
          <Toggle
            on={settings.radio_switch_as_ptt}
            onChange={(v) => update({ radio_switch_as_ptt: v })}
            lg
            aria-label="Radio switch as PTT"
          />
        }
      />
    </Panel>
  );
}
