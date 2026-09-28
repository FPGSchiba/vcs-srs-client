import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";

vi.mock("../../shared/store/useNotificationsSync", () => ({
  useNotificationsSync: () => {},
}));

const markAllNotificationsRead = vi.fn();
const clearNotifications = vi.fn();
const markNotificationRead = vi.fn();
const closeWindow = vi.fn();
const openWindow = vi.fn();
const focusMainWindow = vi.fn();
vi.mock("../../shared/api/client", () => ({
  api: {
    markAllNotificationsRead: () => markAllNotificationsRead(),
    clearNotifications: () => clearNotifications(),
    markNotificationRead: (id: string) => markNotificationRead(id),
    closeWindow: (id: string) => closeWindow(id),
    openWindow: (id: string) => openWindow(id),
    focusMainWindow: () => focusMainWindow(),
  },
}));

import { NotificationsApp } from "./NotificationsApp";
import { useNotifications, emptySnapshot, type NotifyItem } from "../../shared/store/notifications";

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

describe("NotificationsApp", () => {
  beforeEach(() => {
    useNotifications.setState({ snap: emptySnapshot() });
    vi.clearAllMocks();
  });

  it("shows the ALL CLEAR empty state with no items", () => {
    render(<NotificationsApp />);
    expect(screen.getByText("ALL CLEAR")).toBeInTheDocument();
  });

  it("renders one row per item", () => {
    useNotifications.setState({
      snap: { items: [item(), item({ id: "n2", title: "Second", key: "" })], unread: 2 },
    });
    render(<NotificationsApp />);

    expect(screen.getByText("Global hotkeys unavailable")).toBeInTheDocument();
    expect(screen.getByText("Second")).toBeInTheDocument();
  });

  it("populates the filter only from categories actually present", () => {
    useNotifications.setState({ snap: { items: [item()], unread: 1 } });
    render(<NotificationsApp />);

    const options = Array.from(
      (screen.getByRole("combobox") as HTMLSelectElement).options,
    ).map((o) => o.value);

    // The table has seven categories; only `system` is present, so the
    // dropdown must not offer six empty ones.
    expect(options).toEqual(["all", "system"]);
  });

  it("filters by the selected category", () => {
    useNotifications.setState({
      snap: {
        items: [item(), item({ id: "n2", category: "comms", title: "Comms thing" })],
        unread: 2,
      },
    });
    render(<NotificationsApp />);

    fireEvent.change(screen.getByRole("combobox"), { target: { value: "comms" } });

    expect(screen.getByText("Comms thing")).toBeInTheDocument();
    expect(screen.queryByText("Global hotkeys unavailable")).toBeNull();
  });

  it("routes MARK ALL READ and CLEAR ALL through the backend", () => {
    useNotifications.setState({ snap: { items: [item()], unread: 1 } });
    render(<NotificationsApp />);

    fireEvent.click(screen.getByText("MARK ALL READ"));
    fireEvent.click(screen.getByText("CLEAR ALL"));

    // The store is a mirror: every mutation round-trips through Go and
    // comes back on notifications:changed. Nothing is applied optimistically.
    expect(markAllNotificationsRead).toHaveBeenCalledTimes(1);
    expect(clearNotifications).toHaveBeenCalledTimes(1);
    expect(useNotifications.getState().snap.items).toHaveLength(1);
  });

  it("marks an item read when it is expanded", () => {
    useNotifications.setState({ snap: { items: [item()], unread: 1 } });
    render(<NotificationsApp />);

    fireEvent.click(screen.getByText("Global hotkeys unavailable"));

    expect(markNotificationRead).toHaveBeenCalledWith("n1");
  });

  it("dispatches an open-window action by target", () => {
    useNotifications.setState({
      snap: {
        items: [
          item({
            actions: [
              { label: "OPEN COMMS", icon: "comms", kind: "open-window", target: "comms", primary: true },
            ],
          }),
        ],
        unread: 1,
      },
    });
    render(<NotificationsApp />);
    fireEvent.click(screen.getByText("Global hotkeys unavailable"));
    fireEvent.click(screen.getByText("OPEN COMMS"));

    expect(openWindow).toHaveBeenCalledWith("comms");
  });

  it("focuses the main window for a navigate action rather than opening one", () => {
    useNotifications.setState({
      snap: {
        items: [
          item({
            actions: [
              { label: "OPEN KEYBIND SETTINGS", icon: "settings", kind: "navigate", target: "settings", primary: true },
            ],
          }),
        ],
        unread: 1,
      },
    });
    render(<NotificationsApp />);
    fireEvent.click(screen.getByText("Global hotkeys unavailable"));
    fireEvent.click(screen.getByText("OPEN KEYBIND SETTINGS"));

    // openWindow("main") would spawn a SECOND main window: the real one is
    // a Wails window resolved by name, not a Registry entry.
    expect(focusMainWindow).toHaveBeenCalledTimes(1);
    expect(openWindow).not.toHaveBeenCalled();
  });

  it("closes through the Go registry rather than the webview", () => {
    render(<NotificationsApp />);
    fireEvent.click(screen.getByLabelText("close"));

    // Same reasoning as CommsApp: closing through the registry persists
    // geometry and is more reliable than the in-webview Window.Close().
    expect(closeWindow).toHaveBeenCalledWith("notifications");
  });
});
