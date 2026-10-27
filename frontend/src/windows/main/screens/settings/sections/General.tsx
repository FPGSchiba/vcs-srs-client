import { Panel } from "../../../../../shared/components/Panel";
import { SettingRow } from "../../../../../shared/components/SettingRow";
import { Toggle } from "../../../../../shared/components/Toggle";
import type { Settings } from "../../../../../shared/store/settings";
import {
  commitSettings,
  useEffectiveSettings,
} from "../../../../../shared/store/settingsCommit";

/**
 * General renders the General-section toggles from the design prototype's
 * SettingsGeneral, plus "Play notification sounds" -- Phase 7.2's addition,
 * placed next to "Play connection sounds" because the two gate the same kind
 * of thing (an SFX the user may not want) through the same shape of setting.
 *
 * Go is the single source of truth: the store is written only by the
 * hydrate and the backend's `settings:changed` echo. Every toggle goes
 * through `commitSettings`, which sends the full struct built from the
 * newest in-flight request (so two quick changes both survive) and shows
 * that request as `pending` until it settles. A failed save clears
 * `pending`, so the toggle returns to the stored value; Go reports the
 * failure through the notification channel (Phase 6.9 CL3, D11).
 */
export function General() {
  const settings = useEffectiveSettings();

  if (!settings) return null;

  const update = (patch: Partial<Settings>) => {
    void commitSettings((s) => ({ ...s, ...patch }));
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
        label="Play notification sounds"
        desc="SFX when a failure notification arrives."
        control={
          <Toggle
            on={settings.play_notification_sounds}
            onChange={(v) => update({ play_notification_sounds: v })}
            lg
            aria-label="Play notification sounds"
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
