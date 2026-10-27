import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const setSettings = vi.fn();
vi.mock("../../../../../shared/api/client", () => ({
  api: { setSettings: (...a: unknown[]) => setSettings(...a) },
}));

import { General } from "./General";
import { useSettings } from "../../../../../shared/store/settings";
import { usePendingSettings } from "../../../../../shared/store/settingsCommit";

function seed() {
  useSettings.setState({
    settings: {
      start_minimized: false, minimize_to_tray: true, show_transmitter_name: true,
      play_connection_sounds: true, play_notification_sounds: true, radio_switch_as_ptt: false,
      audio: {} as never,
    },
  });
}

describe("General settings", () => {
  beforeEach(() => {
    setSettings.mockReset();
    usePendingSettings.setState({ pending: null });
    seed();
  });
  afterEach(() => vi.restoreAllMocks());

  it("a rejected save flips the toggle back", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    let reject!: (e: unknown) => void;
    setSettings.mockReturnValue(new Promise((_r, rej) => (reject = rej)));
    render(<General />);
    const toggle = screen.getByRole("switch", { name: "Start minimized" });
    expect(toggle).toHaveAttribute("aria-checked", "false");

    await userEvent.click(toggle);
    // The in-flight request shows while it is pending...
    expect(toggle).toHaveAttribute("aria-checked", "true");

    reject(new Error("read-only config directory"));
    // ...and the rejection drops it, so the store's value shows again.
    await waitFor(() => expect(toggle).toHaveAttribute("aria-checked", "false"));
    expect(useSettings.getState().settings?.start_minimized).toBe(false);
  });
});
