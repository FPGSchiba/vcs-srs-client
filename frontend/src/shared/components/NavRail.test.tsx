import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { NavRail } from "./NavRail";
import { useSession } from "../store/session";

const LABELS = [
  "Home", "Operations", "Player List", "Transmission Log", "Radio Profiles",
  "Server Details", "Administration", "Settings", "Support",
];

// FE04: the rail is the primary navigation, so it must work without a mouse.
describe("NavRail keyboard navigation", () => {
  beforeEach(() => {
    useSession.setState({ self: null });
  });

  it("Tab reaches every item and Logout", async () => {
    render(<NavRail activeKey="home" onSelect={() => {}} onLogout={() => {}} />);
    const seen: string[] = [];
    for (let i = 0; i < LABELS.length + 2; i++) {
      await userEvent.tab();
      const el = document.activeElement as HTMLElement;
      seen.push(el.getAttribute("aria-label") ?? el.textContent ?? "");
    }
    for (const label of LABELS) expect(seen).toContain(label);
    expect(seen).toContain("Logout");
  });

  it("Enter and Space activate items and Logout", async () => {
    const onSelect = vi.fn();
    const onLogout = vi.fn();
    render(<NavRail activeKey="home" onSelect={onSelect} onLogout={onLogout} />);
    screen.getByRole("button", { name: "Transmission Log" }).focus();
    await userEvent.keyboard("{Enter}");
    expect(onSelect).toHaveBeenCalledWith("history");
    screen.getByRole("button", { name: "Settings" }).focus();
    await userEvent.keyboard(" ");
    expect(onSelect).toHaveBeenCalledWith("settings");
    screen.getByRole("button", { name: "Logout" }).focus();
    await userEvent.keyboard("{Enter}");
    expect(onLogout).toHaveBeenCalledTimes(1);
  });

  it("marks exactly the active item with aria-current=page", () => {
    const { container } = render(<NavRail activeKey="history" onSelect={() => {}} onLogout={() => {}} />);
    const current = container.querySelectorAll('[aria-current="page"]');
    expect(current).toHaveLength(1);
    expect(current[0]).toBe(screen.getByRole("button", { name: "Transmission Log" }));
  });

  it("the collapse toggle is a named, typed button", async () => {
    render(<NavRail activeKey="home" onSelect={() => {}} />);
    const toggle = screen.getByRole("button", { name: "Collapse" });
    expect(toggle).toHaveAttribute("type", "button");
    await userEvent.click(toggle);
    expect(screen.getByRole("button", { name: "Expand" })).toHaveAttribute("type", "button");
  });
});
