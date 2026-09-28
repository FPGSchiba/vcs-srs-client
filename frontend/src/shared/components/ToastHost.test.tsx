import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { StrictMode } from "react";
import { render, screen, act, fireEvent } from "@testing-library/react";

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
      // other items -- and the real backend delivers a FRESH array on every
      // `notifications:changed` (a freshly deserialized JSON payload), never
      // the same reference twice. Spreading only the top level here would
      // keep `snap.items` as the SAME array, which the `items` selector
      // would never see as a change -- making this assertion vacuous even
      // if the "toasted once" guard were deleted entirely. Rebuild the
      // array so the effect genuinely re-fires.
      useNotifications.setState({ snap: { items: [...snap.items], unread: 1 } });
      useNotifications.setState({ snap: { items: [...snap.items], unread: 1 } });
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

  it("still dismisses a toast under StrictMode when the item was already present at first mount", () => {
    // The more StrictMode-idiomatic reproduction: seed the store BEFORE the
    // first render, not after. StrictMode mounts, cleans up and remounts
    // every effect for that first commit -- the cleanup effect can clear the
    // just-scheduled timer, and since the `seen` ref survives that cycle,
    // the item would never get its timer rescheduled without the repair
    // clause in the items-effect. Without the fix this toast never
    // disappears.
    useNotifications.setState({ snap: { items: [item()], unread: 1 } });
    render(
      <StrictMode>
        <ToastHost />
      </StrictMode>,
    );
    expect(screen.getByText("Global hotkeys unavailable")).toBeInTheDocument();

    act(() => {
      vi.advanceTimersByTime(6500);
    });

    expect(screen.queryByText("Global hotkeys unavailable")).toBeNull();
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

  it("re-toasts when an item changes content in place under the same id", () => {
    render(<ToastHost />);
    act(() => {
      useNotifications.setState({
        snap: { items: [item({ context: [{ key: "PERMISSION", value: "denied" }] })], unread: 1 },
      });
    });
    expect(screen.getByText("no backend")).toBeInTheDocument();

    act(() => {
      // Exactly what Go's raiseLocked does on a DIFFERING fingerprint: same
      // id, new content, re-marked unread -- and the sound fires. Spec 3.8:
      // "it fires on exactly the same items the toast does. One rule, two
      // surfaces." Gated on the id alone this was a sound with nothing new
      // on screen. Spec 5.1's `denied -> granted-but-still-unregistered`
      // transition is exactly this update.
      useNotifications.setState({
        snap: {
          items: [
            item({
              body: "accessibility granted, still unregistered",
              context: [{ key: "PERMISSION", value: "granted" }],
            }),
          ],
          unread: 1,
        },
      });
    });

    expect(screen.getByText("accessibility granted, still unregistered")).toBeInTheDocument();
    expect(screen.queryByText("no backend")).toBeNull();
    // Replaced, not stacked: `key={t.id}` would otherwise be duplicated.
    expect(screen.getAllByText("Global hotkeys unavailable")).toHaveLength(1);
  });

  it("retracts a visible toast when its item resolves", () => {
    render(<ToastHost />);
    act(() => {
      useNotifications.setState({ snap: { items: [item()], unread: 1 } });
    });
    expect(screen.getByText("Global hotkeys unavailable")).toBeInTheDocument();

    act(() => {
      // A microphone error that clears a second later. Left up, the toast
      // would claim a fault that is already gone for another 5.5s.
      vi.advanceTimersByTime(1000);
      useNotifications.setState({
        snap: { items: [item({ resolved: true, unread: false })], unread: 0 },
      });
    });

    expect(screen.queryByText("Global hotkeys unavailable")).toBeNull();
  });

  it("never toasts an already-resolved error the host is seeing for the first time", () => {
    // The HYDRATION case, and the only thing the `!i.resolved` clause of the
    // `fresh` filter defends. Its sibling above -- an item that resolves
    // while its toast is up -- cannot reach it: by then the id is already in
    // `seen`, so the signature check short-circuits and the retraction is
    // done by `live` instead. Dropping `!i.resolved` therefore left every
    // other test in this file green.
    //
    // Real scenario: the main window mounts, or re-hydrates through
    // GetNotifications after a reload, while a microphone error that already
    // FAILED AND RECOVERED is still in the backend's list. `seen` starts
    // empty, so an unguarded filter treats the resolved error as fresh and
    // pops a toast for a fault that has already cleared -- which is exactly
    // the complaint the effect's own comment says it prevents. signature()
    // deliberately excludes `resolved`, so this clause is the only defence.
    render(<ToastHost />);
    act(() => {
      useNotifications.setState({
        snap: { items: [item({ resolved: true, unread: false })], unread: 0 },
      });
    });

    expect(screen.queryByText("Global hotkeys unavailable")).toBeNull();

    // And it must stay absent: no timer was armed, so nothing can surface it
    // on a later tick either.
    act(() => {
      vi.advanceTimersByTime(6500);
    });
    expect(screen.queryByText("Global hotkeys unavailable")).toBeNull();
  });

  it("retracts a visible toast when its item leaves the list", () => {
    render(<ToastHost />);
    act(() => {
      useNotifications.setState({ snap: { items: [item()], unread: 1 } });
    });
    expect(screen.getByText("Global hotkeys unavailable")).toBeInTheDocument();

    act(() => {
      // CLEAR ALL, or a Dismiss from the popout.
      useNotifications.setState({ snap: emptySnapshot() });
    });

    expect(screen.queryByText("Global hotkeys unavailable")).toBeNull();
  });

  it("does not re-arm a dismiss timer for a hand-dismissed toast", () => {
    render(<ToastHost />);
    act(() => {
      useNotifications.setState({ snap: { items: [item()], unread: 1 } });
    });

    const setSpy = vi.spyOn(globalThis, "setTimeout");
    act(() => {
      fireEvent.click(screen.getByLabelText("Dismiss Global hotkeys unavailable"));
    });
    expect(screen.queryByText("Global hotkeys unavailable")).toBeNull();

    act(() => {
      // The item is still present and unresolved backend-side -- the
      // condition has not gone away just because the user closed the toast
      // -- so every subsequent republish used to re-arm a phantom 6.5s
      // timer, forever.
      useNotifications.setState({ snap: { items: [item()], unread: 1 } });
      useNotifications.setState({ snap: { items: [item()], unread: 1 } });
    });

    expect(setSpy.mock.calls.filter((c) => c[1] === 6500)).toHaveLength(0);
    expect(screen.queryByText("Global hotkeys unavailable")).toBeNull();
    setSpy.mockRestore();
  });

  it("re-toasts a hand-dismissed item when its content changes", () => {
    render(<ToastHost />);
    act(() => {
      useNotifications.setState({ snap: { items: [item()], unread: 1 } });
    });
    act(() => {
      fireEvent.click(screen.getByLabelText("Dismiss Global hotkeys unavailable"));
    });
    expect(screen.queryByText("Global hotkeys unavailable")).toBeNull();

    act(() => {
      // The control for the test above: suppressing the re-arm must not
      // become suppressing the item. A dismissal answers the content the
      // user saw; a content change is a new occurrence.
      useNotifications.setState({ snap: { items: [item({ body: "device disappeared" })], unread: 1 } });
    });

    expect(screen.getByText("device disappeared")).toBeInTheDocument();
  });
  it("does not re-arm a dismiss timer for a NATURALLY EXPIRED toast", () => {
    render(<ToastHost />);
    act(() => {
      useNotifications.setState({ snap: { items: [item()], unread: 1 } });
    });
    expect(screen.getByText("Global hotkeys unavailable")).toBeInTheDocument();

    act(() => {
      vi.advanceTimersByTime(6500);
    });
    expect(screen.queryByText("Global hotkeys unavailable")).toBeNull();

    const setSpy = vi.spyOn(globalThis, "setTimeout");
    act(() => {
      // The condition has not gone away just because the toast's dwell ran
      // out, so the item is still present and unresolved. The auto-dismiss
      // path deletes its own timer but the id stays in `seen`, so -- unlike
      // the hand-dismiss path, which was guarded from the start -- every
      // subsequent republish used to arm a fresh 6.5s timer for an invisible
      // item, each firing a setVisible that allocates and re-renders for no
      // visual change.
      useNotifications.setState({ snap: { items: [item()], unread: 1 } });
      useNotifications.setState({ snap: { items: [item()], unread: 1 } });
    });

    expect(setSpy.mock.calls.filter((c) => c[1] === 6500)).toHaveLength(0);
    expect(screen.queryByText("Global hotkeys unavailable")).toBeNull();
    setSpy.mockRestore();
  });

  it("re-toasts a naturally expired item when its content changes", () => {
    render(<ToastHost />);
    act(() => {
      useNotifications.setState({ snap: { items: [item()], unread: 1 } });
    });
    act(() => {
      vi.advanceTimersByTime(6500);
    });
    expect(screen.queryByText("Global hotkeys unavailable")).toBeNull();

    act(() => {
      // The control, matching the hand-dismiss pair above: suppressing the
      // re-arm must not become suppressing the item.
      useNotifications.setState({ snap: { items: [item({ body: "device disappeared" })], unread: 1 } });
    });

    expect(screen.getByText("device disappeared")).toBeInTheDocument();
  });

  it("forgets the bookkeeping for items that leave the list", () => {
    render(<ToastHost />);
    act(() => {
      useNotifications.setState({ snap: { items: [item()], unread: 1 } });
    });
    act(() => {
      fireEvent.click(screen.getByLabelText("Dismiss Global hotkeys unavailable"));
    });

    act(() => {
      // The backend evicts it (its 200-item cap, CLEAR ALL, or a backend-side
      // dismiss). `seen` and `retired` used to keep the id forever.
      useNotifications.setState({ snap: emptySnapshot() });
    });

    act(() => {
      // Ids are never reused, so an id that comes BACK can only be a fresh
      // occurrence -- and must toast, with a full dwell, rather than be
      // filtered out by a stale `seen` entry or a stale retirement.
      useNotifications.setState({ snap: { items: [item()], unread: 1 } });
    });

    expect(screen.getByText("Global hotkeys unavailable")).toBeInTheDocument();
    act(() => {
      vi.advanceTimersByTime(6500);
    });
    expect(screen.queryByText("Global hotkeys unavailable")).toBeNull();
  });
});
