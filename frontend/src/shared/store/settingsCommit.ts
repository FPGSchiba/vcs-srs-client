import { create } from "zustand";
import { api } from "../api/client";
import { useSettings, type Settings } from "./settings";

/**
 * The settings commit path (Phase 6.9 CL3, D11; fixes FE05's lost update and
 * FE09's silent failure for settings).
 *
 * Go stays the single source of truth: `useSettings.settings` is written
 * ONLY by the hydrate and the `settings:changed` echo. `pending` is this
 * window's newest request that has been sent but has not settled yet.
 *
 * - Every commit is built from `pending ?? store`, so two changes made before
 *   either echo lands both survive (the old code spread the render-time
 *   struct, and the second change overwrote the first).
 * - Sections render `pending ?? store` (`useEffectiveSettings`), so a control
 *   moves the moment it is used.
 * - On settle the overlay is cleared, but only if it is still this commit's,
 *   so an older settle never hides a newer in-flight change. A rejection is
 *   therefore the rollback: the store's value shows again. The failure itself
 *   reaches the user through Go's notification channel (`reportAction`),
 *   not through this window.
 */
export const usePendingSettings = create<{ pending: Settings | null }>(() => ({
  pending: null,
}));

export function useEffectiveSettings(): Settings | null {
  const pending = usePendingSettings((s) => s.pending);
  const stored = useSettings((s) => s.settings);
  return pending ?? stored;
}

export function commitSettings(change: (s: Settings) => Settings): Promise<void> {
  const base = usePendingSettings.getState().pending ?? useSettings.getState().settings;
  if (!base) return Promise.resolve();
  const next = change(base);
  usePendingSettings.setState({ pending: next });

  const clear = () => {
    if (usePendingSettings.getState().pending === next) {
      usePendingSettings.setState({ pending: null });
    }
  };
  // An async function body runs synchronously up to its first await, so the
  // request is issued in this tick; a synchronous throw becomes a rejection
  // and a non-promise return (a mock returning undefined) is tolerated.
  // Either way the rejection handler below sees every failure.
  const send = async (): Promise<unknown> => api.setSettings(next);
  const sent = send();
  return sent.then(clear, (err) => {
    clear();
    console.error("setSettings failed; reported via the notification channel", err);
  });
}
