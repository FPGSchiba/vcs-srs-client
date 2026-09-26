import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { StrictMode } from "react";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";

const reconnect = vi.fn();
const reconnectVoice = vi.fn();

vi.mock("../api/client", () => ({
  api: {
    reconnect: () => reconnect(),
    reconnectVoice: () => reconnectVoice(),
  },
}));

import { ConnBanner } from "./ConnBanner";
import { useConnection, emptyConnection, type ConnectionState } from "../store/connection";

const ok = { state: "connected", rtt_ms: 8, healthy: true, available: true, error: "" };
const down = { state: "disconnected", rtt_ms: -1, healthy: false, available: true, error: "" };
const voiceDown = { state: "closed", rtt_ms: -1, healthy: false, available: true, error: "" };
const voiceNA = { state: "unavailable", rtt_ms: -1, healthy: false, available: false, error: "" };

const set = (over: Partial<ConnectionState>) =>
  useConnection.setState({ conn: { server: "127.0.0.1:5002", control: ok, voice: ok, ...over } });

describe("ConnBanner", () => {
  beforeEach(() => {
    reconnect.mockReset().mockResolvedValue(undefined);
    reconnectVoice.mockReset().mockResolvedValue(undefined);
    useConnection.setState({ conn: emptyConnection() });
  });
  afterEach(() => vi.restoreAllMocks());

  it("renders nothing when both planes are healthy", () => {
    set({});
    const { container } = render(<ConnBanner />);
    expect(container.querySelector(".conn-banner")).toBeNull();
  });

  it("renders DISCONNECTED when both are down", () => {
    set({ control: down, voice: voiceDown });
    const { container } = render(<ConnBanner />);

    expect(screen.getByText("DISCONNECTED")).toBeInTheDocument();
    expect(screen.getByText(/FULL RECONNECT/)).toBeInTheDocument();
    expect(container.querySelector(".conn-banner.alert")).not.toBeNull();
  });

  it("renders CONTROL DEGRADED when control alone is down", () => {
    set({ control: down, voice: ok });
    const { container } = render(<ConnBanner />);

    expect(screen.getByText("CONTROL DEGRADED")).toBeInTheDocument();
    expect(screen.getByText(/RECONNECT CONTROL/)).toBeInTheDocument();
    expect(container.querySelector(".conn-banner.warn")).not.toBeNull();
  });

  it("renders VOICE DEGRADED when voice alone is down", () => {
    set({ control: ok, voice: voiceDown });
    const { container } = render(<ConnBanner />);

    expect(screen.getByText("VOICE DEGRADED")).toBeInTheDocument();
    expect(screen.getByText(/RECONNECT VOICE/)).toBeInTheDocument();
    expect(container.querySelector(".conn-banner.warn")).not.toBeNull();
  });

  it("renders nothing when voice is merely unavailable", () => {
    // The normal state on every CGO-less Windows release build. A permanent
    // VOICE DEGRADED there would be advice to reconnect something that was
    // never going to connect.
    set({ control: ok, voice: voiceNA });
    const { container } = render(<ConnBanner />);
    expect(container.querySelector(".conn-banner")).toBeNull();
  });

  it("calls the control reconnect for the control variant", async () => {
    set({ control: down, voice: ok });
    render(<ConnBanner />);

    fireEvent.click(screen.getByText(/RECONNECT CONTROL/));

    await waitFor(() => expect(reconnect).toHaveBeenCalledTimes(1));
    expect(reconnectVoice).not.toHaveBeenCalled();
  });

  it("calls the voice reconnect for the voice variant", async () => {
    set({ control: ok, voice: voiceDown });
    render(<ConnBanner />);

    fireEvent.click(screen.getByText(/RECONNECT VOICE/));

    await waitFor(() => expect(reconnectVoice).toHaveBeenCalledTimes(1));
    expect(reconnect).not.toHaveBeenCalled();
  });
});

describe("ConnBanner reconnect feedback", () => {
  beforeEach(() => {
    reconnect.mockReset();
    reconnectVoice.mockReset().mockResolvedValue(undefined);
    useConnection.setState({ conn: emptyConnection() });
  });

  it("surfaces a failed reconnect's reason", async () => {
    // Before this the banner called `void api.reconnect()` and discarded the
    // result, so a failed reconnect was completely invisible: the button
    // appeared to do nothing at all.
    reconnect.mockRejectedValue(new Error("reconnect dial: connection refused"));
    set({ control: down, voice: voiceDown });
    render(<ConnBanner />);

    fireEvent.click(screen.getByText(/FULL RECONNECT/));

    await waitFor(() =>
      expect(screen.getByText(/connection refused/)).toBeInTheDocument(),
    );
  });

  it("clears a previous failure when a retry is started", async () => {
    reconnect.mockRejectedValueOnce(new Error("connection refused"));
    set({ control: down, voice: voiceDown });
    render(<ConnBanner />);

    fireEvent.click(screen.getByText(/FULL RECONNECT/));
    await waitFor(() => expect(screen.getByText(/connection refused/)).toBeInTheDocument());

    reconnect.mockResolvedValueOnce(undefined);
    fireEvent.click(screen.getByText(/FULL RECONNECT/));

    await waitFor(() => expect(screen.queryByText(/connection refused/)).toBeNull());
  });
});

describe("ConnBanner in-flight guard", () => {
  beforeEach(() => {
    reconnect.mockReset();
    useConnection.setState({ conn: emptyConnection() });
  });

  // Review Focus #5. App.Reconnect re-dials and re-pushes the persisted
  // radios; two in flight race each other's stream generation, and the
  // loser's stream is cancelled under a connection the user thinks is live.
  it("does not fire a second reconnect while the first is in flight", async () => {
    let release!: () => void;
    reconnect.mockReturnValue(
      new Promise<void>((res) => {
        release = res;
      }),
    );
    set({ control: down, voice: voiceDown });
    render(<ConnBanner />);

    const btn = screen.getByText(/FULL RECONNECT/).closest("button")!;
    fireEvent.click(btn);
    fireEvent.click(btn);
    fireEvent.click(btn);

    expect(reconnect).toHaveBeenCalledTimes(1);
    expect(btn).toBeDisabled();

    release();
    await waitFor(() => expect(btn).not.toBeDisabled());
  });
});

/**
 * ConnBanner renders inside StrictMode in the main window root. It holds
 * in-flight and error state across a reconnect, so the double-render must not
 * reset either. See the #30 lesson.
 */
describe("ConnBanner under StrictMode", () => {
  beforeEach(() => {
    reconnect.mockReset().mockResolvedValue(undefined);
    useConnection.setState({ conn: emptyConnection() });
  });

  it("renders one banner and fires one reconnect under the double-render", async () => {
    set({ control: down, voice: voiceDown });
    const { container } = render(
      <StrictMode>
        <ConnBanner />
      </StrictMode>,
    );

    expect(container.querySelectorAll(".conn-banner")).toHaveLength(1);

    fireEvent.click(screen.getByText(/FULL RECONNECT/));
    await waitFor(() => expect(reconnect).toHaveBeenCalledTimes(1));
  });
});
