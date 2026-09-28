import { describe, it, expect, vi, beforeEach } from "vitest";
import { StrictMode } from "react";
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

import { StatusBar } from "./StatusBar";
import { useConnection, emptyConnection, type ConnectionState } from "../store/connection";
import { useNotifications, emptySnapshot } from "../store/notifications";

const snapshot = (over: Partial<ConnectionState> = {}): ConnectionState => ({
  server: "127.0.0.1:5002",
  control: { state: "connected", rtt_ms: 8, healthy: true, available: true, error: "" },
  voice: { state: "connected", rtt_ms: 12, healthy: true, available: true, error: "" },
  ...over,
});

describe("StatusBar", () => {
  beforeEach(() => {
    toggleWindow.mockClear();
    useConnection.setState({ conn: emptyConnection() });
    useNotifications.setState({ snap: emptySnapshot() });
  });

  it("renders both segments of the dual-pill", () => {
    useConnection.setState({ conn: snapshot() });
    const { container } = render(<StatusBar />);

    expect(container.querySelector(".dual-pill")).not.toBeNull();
    expect(container.querySelector(".seg.ctrl")).not.toBeNull();
    expect(container.querySelector(".seg.voice")).not.toBeNull();
    expect(screen.getByText("CTRL")).toBeInTheDocument();
    expect(screen.getByText("VOICE")).toBeInTheDocument();
  });

  it("renders each plane's own latency", () => {
    useConnection.setState({ conn: snapshot() });
    render(<StatusBar />);

    expect(screen.getByText("8ms")).toBeInTheDocument();
    expect(screen.getByText("12ms")).toBeInTheDocument();
  });

  it("colours the dots per plane", () => {
    useConnection.setState({
      conn: snapshot({
        control: { state: "connected", rtt_ms: 8, healthy: true, available: true, error: "" },
        voice: { state: "closed", rtt_ms: -1, healthy: false, available: true, error: "" },
      }),
    });
    const { container } = render(<StatusBar />);

    expect(container.querySelector(".seg.ctrl .d.ok")).not.toBeNull();
    expect(container.querySelector(".seg.voice .d.alert")).not.toBeNull();
  });

  it("dims the voice dot when voice is unavailable", () => {
    // The whole point of the fourth state: on a CGO-less Windows release
    // build this is the NORMAL rendering, and a red dot there would be
    // telling the user something is broken that was never going to run.
    useConnection.setState({
      conn: snapshot({
        voice: { state: "unavailable", rtt_ms: -1, healthy: false, available: false, error: "" },
      }),
    });
    const { container } = render(<StatusBar />);

    expect(container.querySelector(".seg.voice .d.off")).not.toBeNull();
    expect(container.querySelector(".seg.voice .d.alert")).toBeNull();
  });

  it("activates the pill from the keyboard, not just the mouse", () => {
    // SonarCloud typescript:S1082. The design prototype uses a plain div
    // with onClick, so without explicit key handling the pill is
    // unreachable for anyone not using a pointer.
    useConnection.setState({ conn: snapshot() });
    const onNavigate = vi.fn();
    const { container } = render(<StatusBar onNavigate={onNavigate} />);
    const pill = container.querySelector(".dual-pill")!;

    expect(pill.getAttribute("role")).toBe("button");
    expect(pill.getAttribute("tabindex")).toBe("0");

    fireEvent.keyDown(pill, { key: "Enter" });
    expect(onNavigate).toHaveBeenCalledWith("server");

    fireEvent.keyDown(pill, { key: " " });
    expect(onNavigate).toHaveBeenCalledTimes(2);
  });

  it("ignores unrelated keys on the pill", () => {
    useConnection.setState({ conn: snapshot() });
    const onNavigate = vi.fn();
    const { container } = render(<StatusBar onNavigate={onNavigate} />);

    fireEvent.keyDown(container.querySelector(".dual-pill")!, { key: "a" });

    expect(onNavigate).not.toHaveBeenCalled();
  });

  it("activates the NETWORK button from the keyboard", () => {
    useConnection.setState({ conn: snapshot() });
    const onNavigate = vi.fn();
    const { container } = render(<StatusBar onNavigate={onNavigate} />);
    const netBtn = Array.from(container.querySelectorAll(".sb-btn")).find((el) =>
      el.textContent?.includes("NETWORK"),
    )!;

    expect(netBtn.getAttribute("role")).toBe("button");
    fireEvent.keyDown(netBtn, { key: "Enter" });

    expect(onNavigate).toHaveBeenCalledWith("server");
  });

  it("navigates to Server Details when the pill is clicked", () => {
    useConnection.setState({ conn: snapshot() });
    const onNavigate = vi.fn();
    const { container } = render(<StatusBar onNavigate={onNavigate} />);

    fireEvent.click(container.querySelector(".dual-pill")!);

    expect(onNavigate).toHaveBeenCalledWith("server");
  });

  it("wires the ALERTS bell to the notifications popout with its unread count", () => {
    useNotifications.setState({ snap: { items: [], unread: 2 } });
    const { container } = render(<StatusBar />);

    const bell = container.querySelector('[data-bell="alerts"]') as HTMLElement;
    expect(bell).not.toBeNull();
    expect(bell.className).toContain("has-unread");
    expect(bell.querySelector(".sb-bell-count")?.textContent).toBe("2");

    fireEvent.click(bell);
    expect(toggleWindow).toHaveBeenCalledWith("notifications");
  });

  it("drops has-unread and the count when nothing is unread", () => {
    useNotifications.setState({ snap: { items: [], unread: 0 } });
    const { container } = render(<StatusBar />);
    const bell = container.querySelector('[data-bell="alerts"]') as HTMLElement;

    expect(bell.className).not.toContain("has-unread");
    expect(bell.querySelector(".sb-bell-count")).toBeNull();
  });
});

describe("StatusBar latency rendering", () => {
  beforeEach(() => useConnection.setState({ conn: emptyConnection() }));

  // Review Focus #4. -1 means nothing has been measured; 0 means a genuine
  // sub-millisecond measurement. Collapsing them would show a confident
  // "0ms" on a plane that has never answered a probe.
  it("renders an em dash for an unmeasured plane but 0ms for a measured zero", () => {
    useConnection.setState({
      conn: snapshot({
        control: { state: "connected", rtt_ms: 0, healthy: true, available: true, error: "" },
        voice: { state: "connected", rtt_ms: -1, healthy: true, available: true, error: "" },
      }),
    });
    const { container } = render(<StatusBar />);

    expect(container.querySelector(".seg.ctrl")!.textContent).toContain("0ms");
    expect(container.querySelector(".seg.voice")!.textContent).toContain("—");
  });
});

/**
 * StatusBar subscribes to nothing itself — `useConnection` is a plain
 * Zustand selector — but it renders inside StrictMode in both window roots,
 * so the suite pins that it survives the double-render rather than assuming
 * it. See the #30 lesson.
 */
describe("StatusBar under StrictMode", () => {
  beforeEach(() => useConnection.setState({ conn: emptyConnection() }));

  it("renders the same dual-pill under the simulated double-render", () => {
    useConnection.setState({ conn: snapshot() });
    const { container } = render(
      <StrictMode>
        <StatusBar />
      </StrictMode>,
    );

    expect(container.querySelectorAll(".dual-pill")).toHaveLength(1);
    expect(container.querySelectorAll(".seg.ctrl")).toHaveLength(1);
    expect(screen.getByText("8ms")).toBeInTheDocument();
  });
});
