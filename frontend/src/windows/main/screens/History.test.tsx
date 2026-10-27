import { render, screen, fireEvent, waitFor, act } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import React from "react";
import { History } from "./History";
import { useHistory } from "../../../shared/store/history";
import type { HistoryEntry } from "../../../shared/api/client";

let mockHistoryEntries: HistoryEntry[] = [];

// vitest attaches its own handlers to every promise a vi.fn returns (to
// record settled results), which would mark a rejection as handled. Calls
// that a test checks for unhandled rejections go through this, so the
// component receives a promise nobody else has observed.
const fresh = <T,>(p: Promise<T>): Promise<T> => p.then((v) => v);
const exportHistoryCsv = vi.fn(async () => {});

// Node's process, typed locally: the project has no @types/node.
type Listener = (reason: unknown) => void;
const proc = (globalThis as unknown as {
  process: { on(e: string, f: Listener): void; off(e: string, f: Listener): void };
}).process;

const { mockEventHandlers, unsubscribers } = vi.hoisted(() => {
  return {
    mockEventHandlers: new Map<string, Array<{ handler: (data: unknown) => void; live: boolean }>>(),
    unsubscribers: [] as Array<{ fn: ReturnType<typeof vi.fn>; live: boolean }>,
  };
});

vi.mock("../../../shared/api/client", async (orig) => {
  const actual = await orig<typeof import("../../../shared/api/client")>();
  return {
    ...actual,
    api: {
      ...actual.api,
      getHistory: vi.fn(async () => mockHistoryEntries),
      clearHistory: vi.fn(async () => {}),
      exportHistoryCsv: () => fresh(exportHistoryCsv()),
    },
  };
});

vi.mock("@wailsio/runtime", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@wailsio/runtime")>();
  return {
    ...actual,
    Events: {
      On: vi.fn((eventName: string, callback: (e: { data: unknown }) => void) => {
        if (!mockEventHandlers.has(eventName)) {
          mockEventHandlers.set(eventName, []);
        }
        const handlerObj = {
          handler: (data: unknown) => {
            // Only invoke callback if handler is still live
            if (handlerObj.live) {
              callback({ data });
            }
          },
          live: true,
        };
        mockEventHandlers.get(eventName)!.push(handlerObj);

        const unsubscribeFn = vi.fn();
        const unsubObj = { fn: unsubscribeFn, live: true };
        unsubscribers.push(unsubObj);

        return () => {
          handlerObj.live = false;
          unsubObj.live = false;
          unsubscribeFn();
        };
      }),
    },
  };
});

const row = (o: Partial<HistoryEntry> = {}): HistoryEntry => ({
  at: new Date(Date.now() - 60_000).toISOString(),
  sender: "Dabble",
  guid: "g1",
  freq_khz: 118500,
  freq_mhz: 118.5,
  radio: "Fleet Common",
  dur_ms: 3200,
  own: false,
  ...o,
});

describe("History screen", () => {
  beforeEach(() => {
    useHistory.getState().replaceAll([]);
    mockHistoryEntries = [];
    mockEventHandlers.clear();
    unsubscribers.length = 0;
  });

  it("renders sender, channel, frequency and duration", async () => {
    mockHistoryEntries = [row()];
    render(<History />);
    expect(await screen.findByText("Dabble")).toBeInTheDocument();
    expect(screen.getByText("Fleet Common")).toBeInTheDocument();
    expect(screen.getByText("118.500")).toBeInTheDocument();
    expect(screen.getByText("3.2s")).toBeInTheDocument();
  });

  it("has no Replay column and headers are exactly [Time, Sender, Channel, Frequency, Duration]", async () => {
    mockHistoryEntries = [row()];
    render(<History />);
    await screen.findByText("Dabble");
    const headers = screen.getAllByRole("columnheader");
    const headerTexts = headers.map((h) => h.textContent);
    expect(headerTexts).toEqual(["Time", "Sender", "Channel", "Frequency", "Duration"]);
    expect(screen.queryByText(/replay/i)).not.toBeInTheDocument();
  });

  it("renders a global-channel row with the frequency and no invented name", async () => {
    mockHistoryEntries = [row({ radio: "", freq_khz: 200000, freq_mhz: 200 })];
    render(<History />);
    await screen.findAllByText("200.000");
    const cells = screen.getAllByText("200.000");
    expect(cells.some((c) => c.tagName === "TD")).toBe(true);
    // The channel cell should be empty when radio is empty
    const channelCells = screen.getAllByRole("cell").filter((cell) => {
      const text = cell.textContent;
      return cell.previousSibling && cell.nextSibling;
    });
    const globalChannelCell = channelCells.find((cell) => cell.textContent === "");
    expect(globalChannelCell).toBeDefined();
    expect(screen.queryByText("Fleet Common")).not.toBeInTheDocument();
  });

  it("marks own transmissions with the 'own' CSS class", async () => {
    mockHistoryEntries = [row({ own: true, sender: "FPGSchiba" })];
    render(<History />);
    expect(await screen.findByText("FPGSchiba")).toHaveClass("own");
  });

  it("filters by channel", async () => {
    mockHistoryEntries = [
      row({ sender: "Dabble", freq_khz: 118500 }),
      row({ sender: "Elphi", freq_khz: 122750, radio: "Wing" }),
    ];
    render(<History />);
    await screen.findByText("Dabble");
    fireEvent.change(screen.getByLabelText(/channel/i), { target: { value: "122750" } });
    await waitFor(() => expect(screen.queryByText("Dabble")).not.toBeInTheDocument());
    expect(screen.getByText("Elphi")).toBeInTheDocument();
  });

  it("filters by search across sender and channel", async () => {
    mockHistoryEntries = [
      row({ sender: "Dabble" }),
      row({ sender: "Elphi", radio: "Wing" }),
    ];
    render(<History />);
    await screen.findByText("Dabble");
    fireEvent.change(screen.getByPlaceholderText(/search/i), { target: { value: "wing" } });
    await waitFor(() => expect(screen.queryByText("Dabble")).not.toBeInTheDocument());
    expect(screen.getByText("Elphi")).toBeInTheDocument();
  });

  it("applies channel and search filters together (discriminates both dimensions)", async () => {
    // Channel A: Alice and Bob. Channel B: Charlie and David.
    // Search for "Ali" matches only Alice in ChannelA.
    // Search alone: 1 row. Channel alone: 2 rows. Channel+search: 1 row.
    mockHistoryEntries = [
      row({ sender: "Alice", freq_khz: 118500, radio: "ChannelA" }),
      row({ sender: "Bob", freq_khz: 118500, radio: "ChannelA" }),
      row({ sender: "Charlie", freq_khz: 122750, radio: "ChannelB" }),
      row({ sender: "David", freq_khz: 122750, radio: "ChannelB" }),
    ];
    render(<History />);
    await screen.findByText("Bob");

    // Filter by channel A (shows Alice and Bob)
    fireEvent.change(screen.getByLabelText(/channel/i), { target: { value: "118500" } });
    await waitFor(() => expect(screen.queryByText("Charlie")).not.toBeInTheDocument());
    expect(screen.getByText("Alice")).toBeInTheDocument();
    expect(screen.getByText("Bob")).toBeInTheDocument();

    // Search for "Alice" (only matches Alice)
    fireEvent.change(screen.getByPlaceholderText(/search/i), { target: { value: "alice" } });
    // Both filters together: only Alice from ChannelA
    await waitFor(() => expect(screen.queryByText("Bob")).not.toBeInTheDocument());
    expect(screen.getByText("Alice")).toBeInTheDocument();
    expect(screen.queryByText("Charlie")).not.toBeInTheDocument();
    expect(screen.queryByText("David")).not.toBeInTheDocument();
  });

  it("filters by time window (excludes entries outside the selected window)", async () => {
    const recent = row({ sender: "Recent", at: new Date(Date.now() - 60_000).toISOString() });
    const old = row({
      sender: "Old",
      at: new Date(Date.now() - 2 * 3600_000).toISOString(),
    });
    mockHistoryEntries = [recent, old];
    render(<History />);
    await screen.findByText("Recent");
    expect(screen.queryByText("Old")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "4H" }));
    await waitFor(() => expect(screen.getByText("Old")).toBeInTheDocument());
    expect(screen.getByText("Recent")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "ALL" }));
    expect(screen.getByText("Recent")).toBeInTheDocument();
    expect(screen.getByText("Old")).toBeInTheDocument();
  });

  it("renders an empty state rather than a bare table", () => {
    render(<History />);
    expect(screen.getByText(/no transmissions/i)).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });

  it("registers and unsubscribes listeners correctly under StrictMode", async () => {
    mockHistoryEntries = [];
    const { unmount } = render(
      <React.StrictMode>
        <History />
      </React.StrictMode>
    );
    await screen.findByText(/no transmissions/i);

    // StrictMode: each event has 2 total registrations (1 gets unsubscribed, 1 survives)
    const appendedHandlers = mockEventHandlers.get("history:appended") ?? [];
    const clearedHandlers = mockEventHandlers.get("history:cleared") ?? [];
    expect(appendedHandlers).toHaveLength(2);
    expect(clearedHandlers).toHaveLength(2);

    // Exactly 1 live listener per event
    const liveAppended = appendedHandlers.filter((h) => h.live).length;
    const liveCleared = clearedHandlers.filter((h) => h.live).length;
    expect(liveAppended).toBe(1);
    expect(liveCleared).toBe(1);

    // Before unmount: handler can update the store
    const appendedHandler = appendedHandlers.find((h) => h.live)?.handler;
    const newEntry = row({ sender: "NewSender" });
    appendedHandler?.(newEntry);
    const beforeUnmount = useHistory.getState().entries.length;
    expect(beforeUnmount).toBeGreaterThan(0);
    expect(useHistory.getState().entries.some((e) => e.sender === "NewSender")).toBe(true);

    // Unmount
    unmount();

    // All unsubscribers were called
    unsubscribers.forEach((u) => {
      expect(u.fn).toHaveBeenCalledTimes(1);
    });

    // After unmount: no live listeners remain
    const liveAppendedAfter = appendedHandlers.filter((h) => h.live).length;
    const liveClearedAfter = clearedHandlers.filter((h) => h.live).length;
    expect(liveAppendedAfter).toBe(0);
    expect(liveClearedAfter).toBe(0);

    // After unmount: invoking the captured handler must NOT reach the store
    const lenBefore = useHistory.getState().entries.length;
    appendedHandler?.(row({ sender: "AfterUnmount" }));
    expect(useHistory.getState().entries).toHaveLength(lenBefore);
    expect(useHistory.getState().entries.some((e) => e.sender === "AfterUnmount")).toBe(false);
  });

  // FE08: history is a DELTA channel. An append during the hydrate must not
  // vanish under the (older) hydrate result, and must not be duplicated by
  // the re-fetch that already contains it.
  it("an append during the hydrate re-fetches and shows no duplicate", async () => {
    const { api } = await import("../../../shared/api/client");
    const getHistory = vi.mocked(api.getHistory);
    const e = row({ sender: "RaceSender" });
    let resolveFirst!: (v: HistoryEntry[]) => void;
    getHistory
      .mockImplementationOnce(() => new Promise((r) => (resolveFirst = r)))
      .mockImplementationOnce(async () => [e]);
    render(<History />);
    const appended = (mockEventHandlers.get("history:appended") ?? []).find((h) => h.live)!;
    act(() => appended.handler(e));
    await act(async () => {
      resolveFirst([]);
    });
    await waitFor(() => expect(getHistory).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(screen.getAllByText("RaceSender")).toHaveLength(1));
    expect(useHistory.getState().entries).toHaveLength(1);
  });

  // FE09: Go raises history:export on a write failure; the screen only
  // logs, and must not leave the rejection unhandled.
  it("a rejected export produces no unhandled rejection", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    const unhandled = vi.fn();
    proc.on("unhandledRejection", unhandled);
    try {
      exportHistoryCsv.mockImplementationOnce(async () => {
        throw new Error("disk full");
      });
      render(<History />);
      fireEvent.click(screen.getByRole("button", { name: /export csv/i }));
      await act(async () => {
        await new Promise((r) => setTimeout(r, 20));
      });
      expect(exportHistoryCsv).toHaveBeenCalled();
      expect(unhandled).not.toHaveBeenCalled();
    } finally {
      proc.off("unhandledRejection", unhandled);
    }
  });
});
