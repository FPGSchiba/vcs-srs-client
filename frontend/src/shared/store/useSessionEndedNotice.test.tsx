import { describe, it, expect, beforeEach, vi } from "vitest";
import { render } from "@testing-library/react";

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
import { useSession } from "./session";
import { sessionEndedMessage, useSessionEndedNotice } from "./useSessionEndedNotice";

function Harness() {
  useSessionEndedNotice();
  return null;
}

describe("sessionEndedMessage", () => {
  it("maps the reason to the approved copy (D2)", () => {
    expect(sessionEndedMessage("kicked")).toBe("You were removed from the server. Log in again.");
    expect(sessionEndedMessage("banned")).toBe("You are banned from this server.");
    expect(sessionEndedMessage("expired")).toBe("Your session has ended. Log in again.");
    expect(sessionEndedMessage("stale")).toBe("Your session has ended. Log in again.");
    expect(sessionEndedMessage("")).toBe("Your session has ended. Log in again.");
  });
});

describe("useSessionEndedNotice", () => {
  beforeEach(() => {
    handlers.clear();
    useSession.setState({ sessionNotice: null });
  });

  it("sets the notice from auth:session_ended", () => {
    render(<Harness />);
    expect(EV.authSessionEnded).toBe("auth:session_ended");
    emit(EV.authSessionEnded, { reason: "banned" });
    expect(useSession.getState().sessionNotice).toBe("You are banned from this server.");
  });

  it("unsubscribes on unmount", () => {
    const { unmount } = render(<Harness />);
    unmount();
    expect(handlers.get(EV.authSessionEnded)?.size ?? 0).toBe(0);
  });
});
