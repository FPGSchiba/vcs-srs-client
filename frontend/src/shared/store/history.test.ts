import { describe, expect, it, beforeEach } from "vitest";
import { useHistory } from "./history";
import type { HistoryEntry } from "../api/client";

const entry = (i: number): HistoryEntry => ({
  at: `2026-09-30T21:15:${String(i).padStart(2, "0")}Z`,
  sender: "Dabble",
  guid: "g",
  freq_khz: 118500,
  freq_mhz: 118.5,
  radio: "Fleet Common",
  dur_ms: 3200,
  own: false,
});

describe("useHistory", () => {
  beforeEach(() => useHistory.getState().replaceAll([]));

  it("prepends an appended entry so the list stays newest-first", () => {
    useHistory.getState().replaceAll([entry(2), entry(1)]);
    useHistory.getState().append(entry(3));
    expect(useHistory.getState().entries.map((e) => e.at)).toEqual([
      "2026-09-30T21:15:03Z",
      "2026-09-30T21:15:02Z",
      "2026-09-30T21:15:01Z",
    ]);
  });

  it("caps the list so a long session cannot grow the renderer without bound", () => {
    useHistory.getState().replaceAll([]);
    for (let i = 0; i < 2100; i++) useHistory.getState().append(entry(i % 60));
    expect(useHistory.getState().entries.length).toBe(2000);
  });

  it("clears", () => {
    useHistory.getState().replaceAll([entry(1)]);
    useHistory.getState().clear();
    expect(useHistory.getState().entries).toEqual([]);
  });
});
