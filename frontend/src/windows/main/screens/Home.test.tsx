import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const toggleWindow = vi.fn().mockResolvedValue(undefined);
vi.mock("../../../shared/api/client", () => ({
  api: { toggleWindow: (id: string) => toggleWindow(id) },
}));

import { Home } from "./Home";
import { useWindows } from "../../../shared/store/windows";

// FE04: the Comms tile is the only live launcher, so it must be reachable
// and operable from the keyboard; the inert tiles must stay out of the way.
describe("Home launcher keyboard access", () => {
  beforeEach(() => {
    toggleWindow.mockClear();
    useWindows.setState({ open: [] });
  });

  it("Tab reaches the Comms tile and Enter toggles it", async () => {
    render(<Home />);
    await userEvent.tab();
    const focused = document.activeElement as HTMLElement;
    expect(within(focused).getByText("Communications")).toBeInTheDocument();
    await userEvent.keyboard("{Enter}");
    expect(toggleWindow).toHaveBeenCalledWith("comms");
  });

  it("no inert tile is in the tab order", () => {
    render(<Home />);
    const buttons = screen.getAllByRole("button");
    expect(buttons).toHaveLength(1);
    expect(within(buttons[0]).getByText("Communications")).toBeInTheDocument();
    expect(buttons[0]).toHaveAttribute("aria-pressed", "false");
  });
});
