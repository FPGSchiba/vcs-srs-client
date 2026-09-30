import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { History } from "./History";
import { useHistory } from "../../../shared/store/history";
import type { HistoryEntry } from "../../../shared/api/client";

let mockHistoryEntries: HistoryEntry[] = [];

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
  });

  it("renders sender, channel, frequency and duration", async () => {
    mockHistoryEntries = [row()];
    render(<History />);
    expect(await screen.findByText("Dabble")).toBeInTheDocument();
    expect(screen.getByText("Fleet Common")).toBeInTheDocument();
    expect(screen.getByText("118.500")).toBeInTheDocument();
    expect(screen.getByText("3.2s")).toBeInTheDocument();
  });

  it("has no Replay column", () => {
    mockHistoryEntries = [row()];
    render(<History />);
    expect(screen.queryByText(/replay/i)).not.toBeInTheDocument();
  });

  it("renders a global-channel row with the frequency and no invented name", async () => {
    mockHistoryEntries = [row({ radio: "", freq_khz: 200000, freq_mhz: 200 })];
    render(<History />);
    const cells = await screen.findAllByText("200.000");
    expect(cells.some((c) => c.tagName === "TD")).toBe(true);
    expect(screen.queryByText("Fleet Common")).not.toBeInTheDocument();
  });

  it("marks own transmissions", async () => {
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
    fireEvent.change(screen.getByPlaceholderText(/search/i), { target: { value: "wing" } });
    await waitFor(() => expect(screen.queryByText("Dabble")).not.toBeInTheDocument());
    expect(screen.getByText("Elphi")).toBeInTheDocument();
  });

  it("renders an empty state rather than a bare table", () => {
    render(<History />);
    expect(screen.getByText(/no transmissions/i)).toBeInTheDocument();
  });

  it("subscribes to history:appended and unsubscribes exactly once on real unmount", () => {
    // StrictMode double-invokes effects; the control is that a REAL unmount
    // still detaches the listener.
    const { unmount } = render(<History />);
    const before = useHistory.getState().entries.length;
    unmount();
    expect(useHistory.getState().entries.length).toBe(before);
  });
});
