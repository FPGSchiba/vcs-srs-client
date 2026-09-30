import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { Profiles } from "./Profiles";
import { useProfile } from "../../../shared/store/profile";
import type { ProfileSummary } from "../../../shared/api/client";

// vi.mock is hoisted above plain consts, so the spies must be hoisted too.
const {
  listProfiles,
  getProfileState,
  loadProfile,
  deleteProfile,
  saveProfileAs,
  browseProfilesDir,
  openProfilesDir,
  importProfile,
  exportProfile,
} = vi.hoisted(() => ({
  listProfiles: vi.fn(async () => [] as unknown[]),
  getProfileState: vi.fn(async () => ({ active_path: "", active_name: "", dirty: false, dir: "/p" })),
  loadProfile: vi.fn(async (_path: string) => {}),
  deleteProfile: vi.fn(async (_path: string) => {}),
  saveProfileAs: vi.fn(async (_name: string, _desc: string) => {}),
  browseProfilesDir: vi.fn(async () => {}),
  openProfilesDir: vi.fn(async () => {}),
  importProfile: vi.fn(async () => {}),
  exportProfile: vi.fn(async (_path: string) => {}),
}));

vi.mock("../../../shared/api/client", async (orig) => {
  const actual = await orig<typeof import("../../../shared/api/client")>();
  return {
    ...actual,
    api: {
      ...actual.api,
      listProfiles,
      getProfileState,
      loadProfile,
      deleteProfile,
      saveProfileAs,
      browseProfilesDir,
      openProfilesDir,
      importProfile,
      exportProfile,
    },
  };
});

const summary = (o: Partial<ProfileSummary> = {}): ProfileSummary => ({
  path: "/p/fleet-op.vcs.json",
  name: "Fleet Op",
  description: "notes",
  author: "FPGSchiba",
  modified: "2026-09-30 21:00",
  radio_count: 3,
  window: { w: 540, h: 720 },
  blocks: [
    { radio_id: 1, w: 516, h: 180 },
    { radio_id: 2, w: 253, h: 120 },
  ],
  ...o,
});

// The screen re-reads from the backend on mount, so the mocked backend must
// serve the same data the store is seeded with or the refresh would wipe it.
const seed = (profiles: ProfileSummary[], state = EMPTY_STATE) => {
  listProfiles.mockResolvedValue(profiles);
  getProfileState.mockResolvedValue(state);
  useProfile.setState({ profiles, state });
};

const EMPTY_STATE = { active_path: "", active_name: "", dirty: false, dir: "/p" };

describe("Profiles screen", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    seed([]);
  });

  it("renders a row per profile with its radio count", async () => {
    seed([summary()]);
    render(<Profiles />);
    expect(await screen.findByText("Fleet Op")).toBeInTheDocument();
    expect(screen.getByText("3")).toBeInTheDocument();
  });

  it("renders a layout preview rect per stored block, not a preset name", () => {
    seed([summary()]);
    const { container } = render(<Profiles />);
    expect(container.querySelectorAll("svg rect")).toHaveLength(2);
    expect(screen.queryByText(/POWER|STRIP|2×2/)).not.toBeInTheDocument();
  });

  it("marks the active profile", () => {
    seed([summary()], {
      active_path: "/p/fleet-op.vcs.json",
      active_name: "Fleet Op",
      dirty: false,
      dir: "/p",
    });
    render(<Profiles />);
    expect(screen.getByText(/active/i)).toBeInTheDocument();
  });

  it("loads a profile when LOAD is clicked", async () => {
    seed([summary()]);
    render(<Profiles />);
    fireEvent.click(screen.getByRole("button", { name: /^load$/i }));
    await waitFor(() => expect(loadProfile).toHaveBeenCalledWith("/p/fleet-op.vcs.json"));
  });

  it("filters by name", async () => {
    seed([summary(), summary({ name: "Solo Patrol", path: "/p/solo.vcs.json" })]);
    render(<Profiles />);
    fireEvent.change(screen.getByPlaceholderText(/filter profiles/i), {
      target: { value: "solo" },
    });
    await waitFor(() => expect(screen.queryByText("Fleet Op")).not.toBeInTheDocument());
    expect(screen.getByText("Solo Patrol")).toBeInTheDocument();
  });

  it("shows the resolved profiles directory and wires BROWSE and OPEN", async () => {
    render(<Profiles />);
    expect(screen.getByDisplayValue("/p")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /browse/i }));
    await waitFor(() => expect(browseProfilesDir).toHaveBeenCalled());
    fireEvent.click(screen.getByRole("button", { name: /^open$/i }));
    await waitFor(() => expect(openProfilesDir).toHaveBeenCalled());
  });

  it("renders an empty state when there are no profiles", () => {
    render(<Profiles />);
    expect(screen.getByText(/no profiles/i)).toBeInTheDocument();
  });

  it("gives the clickable row keyboard affordances", () => {
    // SonarCloud typescript:S1082 -- a click handler on a non-button
    // element needs role, tabIndex and Enter/Space in the same commit.
    seed([summary()]);
    render(<Profiles />);
    const row = screen.getByText("Fleet Op").closest("tr");
    expect(row).toHaveAttribute("tabindex", "0");
    expect(row).toHaveAttribute("role", "button");
  });
});
