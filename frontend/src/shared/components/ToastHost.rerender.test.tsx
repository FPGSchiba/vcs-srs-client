import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, act } from "@testing-library/react";

import { ToastHost } from "./ToastHost";
import { useNotifications, emptySnapshot, type NotifyItem } from "../store/notifications";

/**
 * Kept in its OWN file because the render counter is a module mock, and
 * `vi.mock` is file-scoped: folding it into ToastHost.test.tsx would silently
 * stub the dismiss button's glyph out of every other test in that file.
 *
 * `Icon` is the counter because nothing else in this tree renders once per
 * ToastHost render. React's bail-out is invisible from the DOM -- a second
 * render pass reconciles to the identical markup and reuses the same nodes --
 * and a `Profiler` cannot see it either: it commits twice either way. A child
 * component's render count is what actually distinguishes the two.
 */
let iconRenders = 0;
vi.mock("./Icon", () => ({
  Icon: () => {
    iconRenders++;
    return null;
  },
}));

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

describe("ToastHost re-render bail-out", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    useNotifications.setState({ snap: emptySnapshot() });
    iconRenders = 0;
  });
  afterEach(() => {
    vi.runOnlyPendingTimers();
    vi.useRealTimers();
  });

  it("does not re-render a second time when an identical snapshot is republished", () => {
    render(<ToastHost />);
    act(() => {
      useNotifications.setState({ snap: { items: [item()], unread: 1 } });
    });

    iconRenders = 0;
    act(() => {
      // A fresh array with identical contents -- what the backend broadcasts
      // whenever some OTHER part of the snapshot changed. The store selector
      // hands ToastHost a new reference, so ONE render is unavoidable. The
      // `visible` updater must then return the SAME array reference, so React
      // bails out instead of committing a second, purely allocational pass.
      useNotifications.setState({ snap: { items: [item()], unread: 1 } });
    });

    expect(iconRenders).toBe(1);
  });
});
