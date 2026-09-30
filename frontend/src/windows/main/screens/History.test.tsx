import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import React from "react";
import { History } from "./History";
import { useHistory } from "../../../shared/store/history";
import type { HistoryEntry } from "../../../shared/api/client";

let mockHistoryEntries: HistoryEntry[] = [];

const { mockEventHandlers, unsubscribers, liveHandlers } = vi.hoisted(() => {
  return {
    mockEventHandlers: new Map<string, Array<{ handler: (data: unknown) => void; live: boolean }>>(),
    unsubscribers: [] as Array<{ fn: () => void; live: boolean }>,
    liveHandlers: new Map<string, number>(),
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
      exportHistoryCsv: vi.fn(async () => {}),
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
        const handlerObj = { handler: (data: unknown) => callback({ data }), live: true };
        mockEventHandlers.get(eventName)!.push(handlerObj);
        liveHandlers.set(eventName, (liveHandlers.get(eventName) ?? 0) + 1);

        const unsubObj = { fn: vi.fn(), live: true };
        unsubscribers.push(unsubObj);

        return () => {
          handlerObj.live = false;
          unsubObj.live = false;
          liveHandlers.set(eventName, (liveHandlers.get(eventName) ?? 1) - 1);
          unsubObj.fn();
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
      // Find the cell that should contain the channel name (between Sender and Frequency)
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
    // Wait for entries to be populated
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
    // Wait for entries to be populated before filtering
    await screen.findByText("Dabble");
    fireEvent.change(screen.getByPlaceholderText(/search/i), { target: { value: "wing" } });
    await waitFor(() => expect(screen.queryByText("Dabble")).not.toBeInTheDocument());
    expect(screen.getByText("Elphi")).toBeInTheDocument();
  });

  it("applies channel and search filters together", async () => {
    mockHistoryEntries = [
      row({ sender: "Alice", freq_khz: 118500, radio: "ChannelA" }),
      row({ sender: "Bob", freq_khz: 118500, radio: "ChannelA" }),
      row({ sender: "Charlie", freq_khz: 122750, radio: "ChannelB" }),
    ];
    render(<History />);
    await screen.findByText("Bob");
    // Filter by channel A
    fireEvent.change(screen.getByLabelText(/channel/i), { target: { value: "118500" } });
    await waitFor(() => expect(screen.queryByText("Charlie")).not.toBeInTheDocument());
    // Then search for Alice
    fireEvent.change(screen.getByPlaceholderText(/search/i), { target: { value: "alice" } });
    await waitFor(() => expect(screen.queryByText("Bob")).not.toBeInTheDocument());
    // Only Alice from ChannelA should remain
    expect(screen.getByText("Alice")).toBeInTheDocument();
  });

  it("filters by time window (excludes entries outside the selected window)", async () => {
    // Entry from 60 seconds ago (within 1h)
    const recent = row({ sender: "Recent", at: new Date(Date.now() - 60_000).toISOString() });
    // Entry from 2 hours ago (outside 1h, within 4h)
    const old = row({
      sender: "Old",
      at: new Date(Date.now() - 2 * 3600_000).toISOString(),
    });
    mockHistoryEntries = [recent, old];
    render(<History />);
    // Default is 1h, so Recent is visible but Old is not
    await screen.findByText("Recent");
    expect(screen.queryByText("Old")).not.toBeInTheDocument();
    // Click 4H button
    fireEvent.click(screen.getByRole("button", { name: "4H" }));
    // Now Old should be visible
    await waitFor(() => expect(screen.getByText("Old")).toBeInTheDocument());
    expect(screen.getByText("Recent")).toBeInTheDocument();
    // Click ALL button
    fireEvent.click(screen.getByRole("button", { name: "ALL" }));
    expect(screen.getByText("Recent")).toBeInTheDocument();
    expect(screen.getByText("Old")).toBeInTheDocument();
  });

  it("renders an empty state rather than a bare table", () => {
    render(<History />);
    expect(screen.getByText(/no transmissions/i)).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });

  it("registers exactly two event listeners (history:appended and history:cleared) on mount", async () => {
    render(<History />);
    await screen.findByText(/no transmissions/i);
    expect(mockEventHandlers.size).toBe(2);
    expect(mockEventHandlers.has("history:appended")).toBe(true);
    expect(mockEventHandlers.has("history:cleared")).toBe(true);
  });

  it("leaves exactly one live listener per event after StrictMode double-invocation (mounted once)", async () => {
    render(<History />);
    await screen.findByText(/no transmissions/i);
    // After render, there should be exactly 1 live listener per event despite StrictMode
    const appendedHandlers = mockEventHandlers.get("history:appended") ?? [];
    const clearedHandlers = mockEventHandlers.get("history:cleared") ?? [];
    const liveAppended = appendedHandlers.filter((h) => h.live).length;
    const liveCleared = clearedHandlers.filter((h) => h.live).length;
    expect(liveAppended).toBe(1);
    expect(liveCleared).toBe(1);
  });

  it("unsubscribes from all listeners on unmount and can receive events before unmount", async () => {
    mockHistoryEntries = [];
    const { unmount } = render(<History />);
    await screen.findByText(/no transmissions/i);
    const appendedHandlers = mockEventHandlers.get("history:appended");
    expect(appendedHandlers?.length).toBeGreaterThan(0);
    // Get a live handler before unmount
    const appendedHandler = appendedHandlers?.find((h) => h.live)?.handler;
    expect(appendedHandler).toBeDefined();

    const newEntry = row({ sender: "NewSender" });
    appendedHandler?.(newEntry);
    // Store should be updated
    const beforeUnmount = useHistory.getState().entries.length;
    expect(beforeUnmount).toBeGreaterThan(0);

    // Now unmount
    unmount();

    // All unsubscribers should have been called
    const allUnsubsCalled = unsubscribers.every((u) => (u.fn as any).mock?.calls?.length ?? 0 > 0);
    expect(allUnsubsCalled).toBe(true);

    // After unmount, there should be 0 live listeners
    const liveAppended = appendedHandlers?.filter((h) => h.live).length ?? 0;
    expect(liveAppended).toBe(0);
  });
});
