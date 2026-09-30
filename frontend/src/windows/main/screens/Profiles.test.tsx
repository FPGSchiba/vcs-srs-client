import { render, screen, fireEvent, waitFor, act } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { Profiles } from "./Profiles";
import { useProfile } from "../../../shared/store/profile";
import type { ProfileSummary } from "../../../shared/api/client";

// vi.mock is hoisted above plain consts, so the spies must be hoisted too.
const {
  listProfiles,
  getProfileState,
  loadProfile,
  deleteProfile,
  renameProfile,
  saveProfileAs,
  saveProfile,
  browseProfilesDir,
  openProfilesDir,
  importProfile,
  exportProfile,
} = vi.hoisted(() => ({
  listProfiles: vi.fn(async () => [] as unknown[]),
  getProfileState: vi.fn(async () => ({ active_path: "", active_name: "", dirty: false, dir: "/p" })),
  loadProfile: vi.fn(async (_path: string) => {}),
  deleteProfile: vi.fn(async (_path: string) => {}),
  renameProfile: vi.fn(async (_path: string, _name: string, _desc: string) => {}),
  saveProfileAs: vi.fn(async (_name: string, _desc: string) => {}),
  saveProfile: vi.fn(async () => {}),
  browseProfilesDir: vi.fn(async () => {}),
  openProfilesDir: vi.fn(async () => {}),
  importProfile: vi.fn(async () => {}),
  exportProfile: vi.fn(async (_path: string) => {}),
}));

// Captures profile:state subscriptions so a test can fire the handler and
// observe unsubscription behaviourally (a dead handler must not reach the store).
const { eventHandlers } = vi.hoisted(() => ({
  eventHandlers: new Map<string, Array<{ fire: (data: unknown) => void }>>(),
}));

vi.mock("@wailsio/runtime", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@wailsio/runtime")>();
  return {
    ...actual,
    Events: {
      On: vi.fn((name: string, cb: (e: { data: unknown }) => void) => {
        let live = true;
        const entry = { fire: (data: unknown) => live && cb({ data }) };
        eventHandlers.set(name, [...(eventHandlers.get(name) ?? []), entry]);
        return () => {
          live = false;
        };
      }),
    },
  };
});

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
      renameProfile,
      saveProfileAs,
      saveProfile,
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
    { radio_id: 1, variant: "vertical" },
    { radio_id: 2, variant: "narrow-v" },
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
    eventHandlers.clear();
    seed([]);
  });

  it("offers SAVE in place only when dirty with an active profile, and calls saveProfile", async () => {
    const active = summary();
    seed([active], { active_path: "", active_name: "", dirty: true, dir: "/p" });
    const { unmount } = render(<Profiles />);
    await screen.findByText("Fleet Op");
    expect(screen.queryByRole("button", { name: /^save$/i })).not.toBeInTheDocument();
    unmount();

    seed([active], { active_path: active.path, active_name: "Fleet Op", dirty: false, dir: "/p" });
    const second = render(<Profiles />);
    await screen.findByText("Fleet Op");
    expect(screen.queryByRole("button", { name: /^save$/i })).not.toBeInTheDocument();
    second.unmount();

    seed([active], { active_path: active.path, active_name: "Fleet Op", dirty: true, dir: "/p" });
    render(<Profiles />);
    fireEvent.click(await screen.findByRole("button", { name: /^save$/i }));
    expect(saveProfile).toHaveBeenCalledTimes(1);
    expect(saveProfileAs).not.toHaveBeenCalled();
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

  it("gives each row a real select button carrying the selected state", () => {
    // The <tr> cannot be a button (it holds other buttons, and a row is table
    // structure), so keyboard selection lives on a native <button> in the name
    // cell; the row click stays as a mouse convenience.
    seed([summary()]);
    render(<Profiles />);
    const pick = screen.getByRole("button", { name: "Fleet Op" });
    expect(pick.tagName).toBe("BUTTON");
    expect(pick.tabIndex).toBe(0);
    expect(pick).toHaveAttribute("aria-pressed", "false");
    const row = pick.closest("tr")!;
    expect(row).not.toHaveAttribute("role");
    expect(row).not.toHaveAttribute("tabindex");
  });

  it.each([
    ["Enter", "{Enter}"],
    ["Space", " "],
  ])("selects the row and shows its detail on %s", async (_label, keys) => {
    seed([summary()]);
    render(<Profiles />);
    expect(screen.queryByText("3 configured")).not.toBeInTheDocument();
    screen.getByRole("button", { name: "Fleet Op" }).focus();
    await userEvent.keyboard(keys);
    expect(screen.getByText("3 configured")).toBeInTheDocument();
    expect(screen.getByText("notes")).toBeInTheDocument();
    expect(screen.getByText("FPGSchiba")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Fleet Op" })).toHaveAttribute("aria-pressed", "true");
  });

  it("selects the row on click and shows its detail", () => {
    seed([summary()]);
    render(<Profiles />);
    fireEvent.click(screen.getByText("Fleet Op"));
    expect(screen.getByText("3 configured")).toBeInTheDocument();
  });

  it("does not select the row when Enter activates a nested button", async () => {
    // userEvent drives the real activation path (Enter on a focused button
    // fires a click that bubbles to the row); only the button's own
    // stopPropagation keeps the row from being selected.
    seed([summary()]);
    render(<Profiles />);
    screen.getByRole("button", { name: /^load$/i }).focus();
    await userEvent.keyboard("{Enter}");
    expect(screen.queryByText("3 configured")).not.toBeInTheDocument();
  });

  it("focuses the new-profile-name input when SAVE CURRENT AS NEW opens it", () => {
    seed([summary()]);
    render(<Profiles />);
    fireEvent.click(screen.getByRole("button", { name: /save current as new/i }));
    expect(screen.getByLabelText("New profile name")).toHaveFocus();
  });

  it("stops following profile:state after unmount", async () => {
    seed([summary()]);
    const { unmount } = render(<Profiles />);
    await act(async () => {});
    const live = eventHandlers.get("profile:state") ?? [];
    expect(live).toHaveLength(1);

    // Before unmount the handler reaches the store synchronously.
    live[0].fire({ active_path: "", active_name: "", dirty: false, dir: "/pushed" });
    expect(useProfile.getState().state.dir).toBe("/pushed");
    // let the refresh that handler kicked off settle before the dead-handler check
    await act(async () => {});

    unmount();
    useProfile.setState({ state: { active_path: "", active_name: "", dirty: false, dir: "/sentinel" } });
    const listed = listProfiles.mock.calls.length;
    live[0].fire({ active_path: "", active_name: "", dirty: false, dir: "/after" });
    await act(async () => {});
    expect(useProfile.getState().state.dir).toBe("/sentinel");
    expect(listProfiles.mock.calls).toHaveLength(listed);
  });
});

describe("Profiles layout preview", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    eventHandlers.clear();
  });

  // Review Focus #5.
  it("draws a profile with no blocks without NaN geometry", async () => {
    seed([summary({ blocks: [] })]);
    render(<Profiles />);
    await act(async () => {});
    // Scoped to the preview: the first <svg> in the document is an Icon.
    const svg = screen.getByTestId("layout-preview");
    expect(svg.querySelectorAll("rect")).toHaveLength(0);
    expect(svg.outerHTML).not.toContain("NaN");
  });

  it("draws one rect per block, sized from the variant", async () => {
    seed([summary({
      blocks: [
        { radio_id: 1, variant: "vertical" },
        { radio_id: 2, variant: "narrow-v" },
      ],
    })]);
    render(<Profiles />);
    await act(async () => {});
    const rects = screen.getByTestId("layout-preview").querySelectorAll("rect");
    expect(rects).toHaveLength(2);
    expect(Number(rects[0].getAttribute("width"))).toBeGreaterThan(
      Number(rects[1].getAttribute("width")),
    );
  });
});

describe("Profiles delete", () => {
  const A = "/p/fleet-op.vcs.json";
  const B = "/p/solo.vcs.json";
  const advance = (ms: number) => act(async () => void vi.advanceTimersByTime(ms));

  beforeEach(async () => {
    vi.clearAllMocks();
    eventHandlers.clear();
    vi.useFakeTimers({ toFake: ["Date", "setTimeout", "clearTimeout"] });
    seed([summary(), summary({ name: "Solo Patrol", path: B })]);
    render(<Profiles />);
    await act(async () => {});
  });
  afterEach(() => vi.useRealTimers());

  const del = (name: string) => screen.getByRole("button", { name: `Delete ${name}` });
  const confirm = (name: string) => screen.getByRole("button", { name: `Confirm delete ${name}` });

  it("first click arms without deleting and shows CONFIRM", () => {
    fireEvent.click(del("Fleet Op"));
    expect(deleteProfile).not.toHaveBeenCalled();
    expect(confirm("Fleet Op")).toHaveTextContent("CONFIRM?");
  });

  it("second click after the cooling window deletes that exact path", async () => {
    fireEvent.click(del("Fleet Op"));
    await advance(400);
    fireEvent.click(confirm("Fleet Op"));
    expect(deleteProfile).toHaveBeenCalledTimes(1);
    expect(deleteProfile).toHaveBeenCalledWith(A);
  });

  it("arming A then clicking the other row's delete arms only that row and deletes nothing", async () => {
    fireEvent.click(del("Fleet Op"));
    await advance(400);
    fireEvent.click(del("Solo Patrol"));
    expect(deleteProfile).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Delete Fleet Op" })).toBeInTheDocument();
    expect(confirm("Solo Patrol")).toBeInTheDocument();
  });

  it("blur disarms, and the next click re-arms instead of deleting", async () => {
    fireEvent.click(del("Fleet Op"));
    await advance(400);
    fireEvent.blur(confirm("Fleet Op"));
    expect(screen.getByRole("button", { name: "Delete Fleet Op" })).toBeInTheDocument();
    fireEvent.click(del("Fleet Op"));
    expect(deleteProfile).not.toHaveBeenCalled();
    expect(confirm("Fleet Op")).toBeInTheDocument();
  });

  it("ignores a second click inside the cooling window (double-click)", async () => {
    fireEvent.click(del("Fleet Op"));
    await advance(100);
    fireEvent.click(confirm("Fleet Op"));
    expect(deleteProfile).not.toHaveBeenCalled();
    // still armed: a deliberate click after the window deletes
    await advance(300);
    fireEvent.click(confirm("Fleet Op"));
    expect(deleteProfile).toHaveBeenCalledWith(A);
  });

  it("disarms by itself after the arm timeout", async () => {
    fireEvent.click(del("Fleet Op"));
    await advance(3100);
    expect(screen.getByRole("button", { name: "Delete Fleet Op" })).toBeInTheDocument();
    fireEvent.click(del("Fleet Op"));
    expect(deleteProfile).not.toHaveBeenCalled();
  });
});

describe("Profiles rename", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    eventHandlers.clear();
  });

  const openRename = () => {
    seed([summary()]);
    render(<Profiles />);
    fireEvent.click(screen.getByRole("button", { name: "Rename Fleet Op" }));
    return screen.getByLabelText("Profile name");
  };

  it("focuses the rename input when it opens", () => {
    expect(openRename()).toHaveFocus();
  });

  it("does not rename to an empty or whitespace name", () => {
    const input = openRename();
    fireEvent.change(input, { target: { value: "   " } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(renameProfile).not.toHaveBeenCalled();
  });

  it("renames with path, trimmed name and the existing description", async () => {
    const input = openRename();
    fireEvent.change(input, { target: { value: " Wing Op " } });
    fireEvent.keyDown(input, { key: "Enter" });
    await waitFor(() =>
      expect(renameProfile).toHaveBeenCalledWith("/p/fleet-op.vcs.json", "Wing Op", "notes"),
    );
    await waitFor(() => expect(screen.queryByLabelText("Profile name")).not.toBeInTheDocument());
  });

  it("Escape cancels without calling rename", () => {
    const input = openRename();
    fireEvent.change(input, { target: { value: "Other" } });
    fireEvent.keyDown(input, { key: "Escape" });
    expect(renameProfile).not.toHaveBeenCalled();
    expect(screen.queryByLabelText("Profile name")).not.toBeInTheDocument();
  });
});
