import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { StrictMode } from "react";
import { render, waitFor } from "@testing-library/react";

const getConnectionState = vi.fn();

vi.mock("../api/client", () => ({
  api: { getConnectionState: () => getConnectionState() },
}));

const handlers = new Map<string, Set<(d: unknown) => void>>();
const emit = (name: string, data: unknown) => handlers.get(name)?.forEach((h) => h(data));

vi.mock("../api/events", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../api/events")>();
  return {
    EV: actual.EV,
    on: (name: string, cb: (d: unknown) => void) => {
      const set = handlers.get(name) ?? new Set();
      set.add(cb);
      handlers.set(name, set);
      return () => set.delete(cb);
    },
  };
});

import { EV } from "../api/events";
import { useConnection, emptyConnection, type ConnectionState } from "./connection";
import { useConnectionSync } from "./useConnectionSync";

const snapshot = (over: Partial<ConnectionState> = {}): ConnectionState => ({
  server: "127.0.0.1:5002",
  control: { state: "connected", rtt_ms: 8, healthy: true, available: true, error: "" },
  voice: { state: "connected", rtt_ms: 12, healthy: true, available: true, error: "" },
  ...over,
});

function Probe() {
  useConnectionSync();
  return null;
}

describe("useConnectionSync", () => {
  beforeEach(() => {
    handlers.clear();
    getConnectionState.mockReset().mockResolvedValue(snapshot());
    useConnection.setState({ conn: emptyConnection() });
  });
  afterEach(() => vi.restoreAllMocks());

  it("hydrates the store on mount", async () => {
    render(<Probe />);
    await waitFor(() => expect(useConnection.getState().conn.server).toBe("127.0.0.1:5002"));
  });

  it("keeps the store live via connection:state", async () => {
    render(<Probe />);
    await waitFor(() => expect(getConnectionState).toHaveBeenCalled());

    emit(EV.connectionState, snapshot({
      control: { state: "disconnected", rtt_ms: -1, healthy: false, available: true, error: "" },
    }));
    expect(useConnection.getState().conn.control.state).toBe("disconnected");
  });

  it("unsubscribes on unmount so a remount cannot double-handle", () => {
    const { unmount } = render(<Probe />);
    unmount();
    emit(EV.connectionState, snapshot({ server: "elsewhere:1" }));
    expect(useConnection.getState().conn.server).not.toBe("elsewhere:1");
  });

  it("logs rather than swallows a getConnectionState rejection", async () => {
    // Swallowed, this leaves the surface showing the honest-but-frozen
    // empty state with nothing to explain why it never updates.
    const err = vi.spyOn(console, "error").mockImplementation(() => {});
    getConnectionState.mockRejectedValue(new Error("backend unreachable"));

    render(<Probe />);
    await waitFor(() => expect(err).toHaveBeenCalled());
  });
});

/**
 * Both window roots render inside `React.StrictMode` (frontend/src/main.tsx
 * and comms.tsx), which in development runs every effect as
 * setup -> cleanup -> setup on the same element. The suite above renders
 * bare, so it does not exercise the mode the app actually runs in — the gap
 * that hid a total keybind-capture break through two phases (#30).
 *
 * This hook's effect cleanup calls the Wails unsubscribe, which is a real
 * side effect, so it is exactly the shape that broke before.
 */
describe("useConnectionSync under StrictMode", () => {
  beforeEach(() => {
    handlers.clear();
    getConnectionState.mockReset().mockResolvedValue(snapshot());
    useConnection.setState({ conn: emptyConnection() });
  });

  it("still receives events after the simulated unmount and re-mount", async () => {
    render(
      <StrictMode>
        <Probe />
      </StrictMode>,
    );
    await waitFor(() => expect(getConnectionState).toHaveBeenCalled());

    emit(EV.connectionState, snapshot({ server: "after-strict:1" }));
    await waitFor(() =>
      expect(useConnection.getState().conn.server).toBe("after-strict:1"),
    );
  });

  it("leaves exactly one live subscription, not two", async () => {
    // StrictMode's setup -> cleanup -> setup must net out at one handler. Two
    // would double-apply every snapshot — harmless for an idempotent set, but
    // it means the cleanup is not actually removing what the setup added, and
    // the next hook with non-idempotent handling inherits a live bug.
    render(
      <StrictMode>
        <Probe />
      </StrictMode>,
    );
    await waitFor(() => expect(getConnectionState).toHaveBeenCalled());

    expect(handlers.get(EV.connectionState)?.size).toBe(1);
  });

  // The control: a genuine unmount must still unsubscribe. Without this, a
  // fix that simply stopped cleaning up would pass the two tests above.
  it("still unsubscribes on a real unmount", async () => {
    const { unmount } = render(
      <StrictMode>
        <Probe />
      </StrictMode>,
    );
    await waitFor(() => expect(getConnectionState).toHaveBeenCalled());

    unmount();

    expect(handlers.get(EV.connectionState)?.size ?? 0).toBe(0);
  });
});

describe("useConnectionSync hydrate race", () => {
  beforeEach(() => {
    handlers.clear();
    useConnection.setState({ conn: emptyConnection() });
  });

  // Review Focus #3. The mount-time fetch is a promise; the backend can push
  // a newer snapshot while it is still in flight. Letting the late resolution
  // write unconditionally rolls the store back to a state that is already
  // wrong, and nothing is guaranteed to correct it until the next 5s tick.
  it("does not let a late hydrate clobber a newer pushed snapshot", async () => {
    let resolveHydrate!: (c: ConnectionState) => void;
    getConnectionState.mockReset().mockReturnValue(
      new Promise<ConnectionState>((res) => {
        resolveHydrate = res;
      }),
    );

    render(<Probe />);
    await waitFor(() => expect(getConnectionState).toHaveBeenCalled());

    // The push wins the race.
    emit(EV.connectionState, snapshot({ server: "newer:2" }));
    expect(useConnection.getState().conn.server).toBe("newer:2");

    // The stale hydrate resolves afterwards and must be ignored.
    resolveHydrate(snapshot({ server: "stale:1" }));
    await waitFor(() => expect(getConnectionState).toHaveBeenCalled());

    expect(useConnection.getState().conn.server).toBe("newer:2");
  });
});
