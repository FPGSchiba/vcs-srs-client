import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";

const toggleWindow = vi.fn();

vi.mock("../api/client", () => ({
  api: {
    toggleWindow: (...args: unknown[]) => toggleWindow(...args),
  },
}));

vi.mock("../hooks/useBuildInfo", () => ({
  useBuildInfo: () => ({ client_version: "0.1.0", protocol_version: "1", build: "dev" }),
}));

import { TopBar } from "./TopBar";
import { useSession } from "../store/session";
import { useWindows } from "../store/windows";
import { useNotifications, emptySnapshot } from "../store/notifications";

describe("TopBar", () => {
  beforeEach(() => {
    toggleWindow.mockClear();
    useSession.setState({ conn: "connected", self: null });
    useWindows.setState({ open: [] });
    useNotifications.setState({ snap: emptySnapshot() });
  });

  it("renders the current view title", () => {
    render(<TopBar view="home" />);
    expect(screen.getByText("Home")).toBeInTheDocument();
  });

  it("wires the notifications launcher and shows the unread badge", () => {
    useNotifications.setState({ snap: { items: [], unread: 3 } });
    const { container } = render(<TopBar view="home" />);

    const launcher = container.querySelector('[data-launcher="notifications"]') as HTMLElement;
    expect(launcher).not.toBeNull();
    expect(launcher.getAttribute("aria-disabled")).toBeNull();
    expect(launcher.querySelector(".badge")?.textContent).toBe("3");

    fireEvent.click(launcher);
    expect(toggleWindow).toHaveBeenCalledWith("notifications");
  });

  it("gives the notifications launcher a keyboard path", () => {
    const { container } = render(<TopBar view="home" />);
    const launcher = container.querySelector('[data-launcher="notifications"]') as HTMLElement;

    // typescript:S1082 -- touching this line makes it new code for the gate.
    expect(launcher.getAttribute("role")).toBe("button");
    expect(launcher.getAttribute("tabindex")).toBe("0");

    fireEvent.keyDown(launcher, { key: "Enter" });
    expect(toggleWindow).toHaveBeenCalledWith("notifications");
  });

  it("hides the badge when nothing is unread", () => {
    useNotifications.setState({ snap: { items: [], unread: 0 } });
    const { container } = render(<TopBar view="home" />);
    const launcher = container.querySelector('[data-launcher="notifications"]') as HTMLElement;
    expect(launcher.querySelector(".badge")).toBeNull();
  });
});
