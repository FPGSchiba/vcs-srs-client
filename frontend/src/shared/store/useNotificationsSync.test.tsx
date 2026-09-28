import { describe, it, expect, vi, beforeEach } from "vitest";
import { StrictMode } from "react";
import { render, waitFor } from "@testing-library/react";

const listeners: Record<string, (data: unknown) => void> = {};
const offSpy = vi.fn();

vi.mock("../api/events", async () => {
  const actual = await vi.importActual<typeof import("../api/events")>("../api/events");
  return {
    ...actual,
    on: (name: string, cb: (data: unknown) => void) => {
      listeners[name] = cb;
      return () => {
        delete listeners[name];
        offSpy();
      };
    },
  };
});

const getNotifications = vi.fn();
vi.mock("../api/client", () => ({
  api: { getNotifications: () => getNotifications() },
}));

import { useNotificationsSync } from "./useNotificationsSync";
import { useNotifications, emptySnapshot } from "./notifications";
import { EV } from "../api/events";

function Harness() {
  useNotificationsSync();
  return null;
}

const snap = (unread: number) => ({
  items: [
    {
      id: "n1",
      key: "hotkeys.global",
      category: "system",
      severity: "error",
      icon: "bolt",
      title: "Global hotkeys unavailable",
      body: "no backend",
      context: [{ key: "PERMISSION", value: "denied" }],
      actions: [],
      time: "2026-09-28T12:00:00Z",
      unread: unread > 0,
      resolved: false,
    },
  ],
  unread,
});

describe("useNotificationsSync", () => {
  beforeEach(() => {
    useNotifications.setState({ snap: emptySnapshot() });
    offSpy.mockClear();
    getNotifications.mockReset();
    getNotifications.mockResolvedValue(snap(1));
    for (const k of Object.keys(listeners)) delete listeners[k];
  });

  it("hydrates from the backend on mount", async () => {
    render(<Harness />);
    await waitFor(() => {
      expect(useNotifications.getState().snap.unread).toBe(1);
    });
  });

  it("keeps the store live via notifications:changed", async () => {
    render(<Harness />);
    await waitFor(() => expect(listeners[EV.notifications]).toBeDefined());

    listeners[EV.notifications](snap(0));

    expect(useNotifications.getState().snap.unread).toBe(0);
  });

  it("survives StrictMode's simulated unmount with a live subscription", async () => {
    render(
      <StrictMode>
        <Harness />
      </StrictMode>,
    );
    await waitFor(() => expect(listeners[EV.notifications]).toBeDefined());

    // StrictMode mounts, unmounts and remounts. If the cleanup tore down a
    // subscription the remount did not replace, the store would go deaf --
    // the exact failure that hid a total keybind-capture break for two
    // phases.
    listeners[EV.notifications](snap(7));
    expect(useNotifications.getState().snap.unread).toBe(7);
  });

  it("unsubscribes exactly once on a real unmount", async () => {
    const { unmount } = render(<Harness />);
    await waitFor(() => expect(listeners[EV.notifications]).toBeDefined());
    offSpy.mockClear();

    unmount();

    // The control for the StrictMode test above: proving the subscription
    // survives a simulated unmount is only meaningful alongside proof that
    // a REAL unmount still releases it.
    expect(offSpy).toHaveBeenCalledTimes(1);
    expect(listeners[EV.notifications]).toBeUndefined();
  });

  it("leaves the store empty when hydration fails", async () => {
    getNotifications.mockRejectedValue(new Error("not wired"));
    render(<Harness />);
    await waitFor(() => {
      expect(useNotifications.getState().snap.items).toEqual([]);
    });
  });
});
