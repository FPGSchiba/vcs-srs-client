import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";

const requestHotkeyPermission = vi.fn();
const openHotkeyPermissionSettings = vi.fn();
const recheckHotkeyPermission = vi.fn();
vi.mock("../../../../../../shared/api/client", () => ({
  api: {
    requestHotkeyPermission: () => requestHotkeyPermission(),
    openHotkeyPermissionSettings: () => openHotkeyPermissionSettings(),
    recheckHotkeyPermission: () => recheckHotkeyPermission(),
  },
}));

import { PermissionCard } from "./PermissionCard";
import { useSettings } from "../../../../../../shared/store/settings";

const setHotkeys = (over: Partial<ReturnType<typeof useSettings.getState>["hotkeys"]>) =>
  useSettings.setState((s) => ({ hotkeys: { ...s.hotkeys, ...over } }));

describe("PermissionCard", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    requestHotkeyPermission.mockResolvedValue({ prompted: true, permission: "unknown" });
    setHotkeys({ registered: true, error: "", failed: {}, permission: "not_applicable" });
  });

  it("renders nothing when there is nothing to grant", () => {
    // Windows and Linux/X11 report not_applicable; a button would be a dead
    // end, so the card must not render at all.
    const { container } = render(<PermissionCard />);
    expect(container.firstChild).toBeNull();
  });

  it("renders nothing for unknown", () => {
    setHotkeys({ permission: "unknown" });
    const { container } = render(<PermissionCard />);
    expect(container.firstChild).toBeNull();
  });

  it("renders GRANT ACCESS when permission is denied", () => {
    setHotkeys({ permission: "denied", registered: false, error: "not trusted" });
    render(<PermissionCard />);

    expect(screen.getByText("GRANT ACCESS")).toBeInTheDocument();
    expect(screen.queryByText("RE-CHECK")).toBeNull();
  });

  it("stays visible while the grant is still missing even after a re-check", () => {
    // Keyed on permission, not on !registered: the card must not vanish the
    // instant registration succeeds while the grant is still absent.
    setHotkeys({ permission: "denied", registered: true });
    render(<PermissionCard />);
    expect(screen.getByText("GRANT ACCESS")).toBeInTheDocument();
  });

  it("unlocks RE-CHECK after a request and swaps to OPEN SETTINGS once the prompt is spent", async () => {
    setHotkeys({ permission: "denied", registered: false });
    requestHotkeyPermission.mockResolvedValue({ prompted: false, permission: "denied" });
    render(<PermissionCard />);

    fireEvent.click(screen.getByText("GRANT ACCESS"));

    await waitFor(() => expect(screen.getByText("OPEN SETTINGS")).toBeInTheDocument());
    expect(screen.getByText("RE-CHECK")).toBeInTheDocument();
  });

  it("never treats prompted as a grant", async () => {
    setHotkeys({ permission: "denied", registered: false });
    requestHotkeyPermission.mockResolvedValue({ prompted: true, permission: "unknown" });
    render(<PermissionCard />);

    fireEvent.click(screen.getByText("GRANT ACCESS"));

    // macOS resolves the prompt asynchronously and never reports the
    // decision through this call; the answer only ever arrives on
    // hotkeys:state. So GRANT ACCESS must stay, not become OPEN SETTINGS.
    await waitFor(() => expect(screen.getByText("RE-CHECK")).toBeInTheDocument());
    expect(screen.getByText("GRANT ACCESS")).toBeInTheDocument();
    expect(screen.queryByText("OPEN SETTINGS")).toBeNull();
  });

  it("shows the restart hint when access is granted but registration still fails", () => {
    setHotkeys({ permission: "granted", registered: false });
    render(<PermissionCard />);
    expect(screen.getByText(/restart VCS/i)).toBeInTheDocument();
  });
});
