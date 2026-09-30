import { describe, expect, it, beforeEach } from "vitest";
import { useProfile } from "./profile";

describe("useProfile", () => {
  beforeEach(() =>
    useProfile.setState({
      state: { active_path: "", active_name: "", dirty: false, dir: "" },
      profiles: [],
    }),
  );

  it("holds the state the backend sends", () => {
    useProfile.getState().setState({
      active_path: "/p/fleet.vcs.json",
      active_name: "Fleet",
      dirty: true,
      dir: "/p",
    });
    expect(useProfile.getState().state.dirty).toBe(true);
    expect(useProfile.getState().state.active_name).toBe("Fleet");
  });

  it("never holds a nil profile list", () => {
    // The backend returns [] rather than null, but a store default of
    // undefined would still crash .map on first render.
    expect(Array.isArray(useProfile.getState().profiles)).toBe(true);
  });
});
