import { describe, it, expect, vi, beforeEach } from "vitest";
import { StrictMode } from "react";
import { render, screen, fireEvent } from "@testing-library/react";

vi.mock("../hooks/useBuildInfo", () => ({
  useBuildInfo: () => ({ client_version: "0.1.0", protocol_version: "1", build: "dev" }),
}));

import { StatusBar } from "./StatusBar";
import { useConnection, emptyConnection, type ConnectionState } from "../store/connection";

const snapshot = (over: Partial<ConnectionState> = {}): ConnectionState => ({
  server: "127.0.0.1:5002",
  control: { state: "connected", rtt_ms: 8, healthy: true, available: true, error: "" },
  voice: { state: "connected", rtt_ms: 12, healthy: true, available: true, error: "" },
  ...over,
});

describe("StatusBar", () => {
  beforeEach(() => useConnection.setState({ conn: emptyConnection() }));

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

  it("navigates to Server Details when the pill is clicked", () => {
    useConnection.setState({ conn: snapshot() });
    const onNavigate = vi.fn();
    const { container } = render(<StatusBar onNavigate={onNavigate} />);

    fireEvent.click(container.querySelector(".dual-pill")!);

    expect(onNavigate).toHaveBeenCalledWith("server");
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
