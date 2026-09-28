import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { StrictMode } from "react";
import { render, screen, act } from "@testing-library/react";

import { ToastHost } from "./ToastHost";
import { useNotifications, emptySnapshot, type NotifyItem } from "../store/notifications";

const item = (over: Partial<NotifyItem> = {}): NotifyItem => ({
  id: "n1",
  key: "hotkeys.global",
  category: "system",
  severity: "error",
  icon: "bolt",
  title: "Global hotkeys unavailable",
  body: "no backend",
  context: [],
  actions: [],
  time: "2026-09-28T12:00:00Z",
  unread: true,
  resolved: false,
  ...over,
});

describe("ToastHost", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    useNotifications.setState({ snap: emptySnapshot() });
  });
  afterEach(() => {
    vi.runOnlyPendingTimers();
    vi.useRealTimers();
  });

  it("toasts an error-severity item", () => {
    render(<ToastHost />);
    act(() => {
      useNotifications.setState({ snap: { items: [item()], unread: 1 } });
    });

    expect(screen.getByText("Global hotkeys unavailable")).toBeInTheDocument();
  });

  it("does not toast warn or info", () => {
    render(<ToastHost />);
    act(() => {
      useNotifications.setState({
        snap: {
          items: [
            item({ id: "w", severity: "warn", title: "warned" }),
            item({ id: "i", severity: "info", title: "informed", unread: false }),
          ],
          unread: 1,
        },
      });
    });

    expect(screen.queryByText("warned")).toBeNull();
    expect(screen.queryByText("informed")).toBeNull();
  });

  it("auto-dismisses after 6.5s", () => {
    render(<ToastHost />);
    act(() => {
      useNotifications.setState({ snap: { items: [item()], unread: 1 } });
    });
    expect(screen.getByText("Global hotkeys unavailable")).toBeInTheDocument();

    act(() => {
      vi.advanceTimersByTime(6500);
    });

    expect(screen.queryByText("Global hotkeys unavailable")).toBeNull();
  });

  it("toasts an item only once, however often the snapshot is republished", () => {
    render(<ToastHost />);
    const snap = { items: [item()], unread: 1 };

    act(() => {
      useNotifications.setState({ snap });
    });
    act(() => {
      // A full Snapshot is broadcast on EVERY change, including changes to
      // other items. Re-toasting on each would turn one failure into a
      // stream of identical toasts.
      useNotifications.setState({ snap: { ...snap } });
      useNotifications.setState({ snap: { ...snap } });
    });

    expect(screen.getAllByText("Global hotkeys unavailable")).toHaveLength(1);
  });

  it("schedules exactly one dismiss timer under StrictMode", () => {
    const setSpy = vi.spyOn(globalThis, "setTimeout");
    render(
      <StrictMode>
        <ToastHost />
      </StrictMode>,
    );
    setSpy.mockClear();

    act(() => {
      useNotifications.setState({ snap: { items: [item()], unread: 1 } });
    });

    // StrictMode mounts, unmounts and remounts every effect. A dismiss
    // timer scheduled per mount would fire twice and could dismiss a toast
    // that a later item had replaced. This is the exact shape that broke
    // keybind capture silently for two phases.
    const dismissTimers = setSpy.mock.calls.filter((c) => c[1] === 6500);
    expect(dismissTimers).toHaveLength(1);
    setSpy.mockRestore();
  });

  it("clears its timers exactly once on a real unmount", () => {
    const clearSpy = vi.spyOn(globalThis, "clearTimeout");
    const { unmount } = render(<ToastHost />);
    act(() => {
      useNotifications.setState({ snap: { items: [item()], unread: 1 } });
    });
    clearSpy.mockClear();

    unmount();

    // The control for the StrictMode test above. Proving no double-schedule
    // is only meaningful alongside proof that a real unmount still releases
    // the timer -- otherwise it would fire into an unmounted tree.
    expect(clearSpy).toHaveBeenCalledTimes(1);
    clearSpy.mockRestore();
  });
});
