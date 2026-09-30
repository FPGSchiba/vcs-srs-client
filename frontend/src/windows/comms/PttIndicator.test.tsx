import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { PttIndicator } from "./PttIndicator";

describe("PttIndicator", () => {
  it("is an inert indicator, not a control", () => {
    render(<PttIndicator w={96} h={28} transmitting={false} showLabel />);
    const b = screen.getByRole("button");
    expect(b).toBeDisabled();
    expect(b.getAttribute("title")).toContain("keybind");
  });

  it("takes its box from the descriptor", () => {
    render(<PttIndicator w={64} h={64} transmitting={false} showLabel={false} />);
    expect(screen.getByRole("button")).toHaveStyle({ width: "64px", height: "64px" });
  });

  it("spans the content width when the descriptor says fill", () => {
    render(<PttIndicator w="fill" h={26} transmitting={false} showLabel={false} />);
    expect(screen.getByRole("button")).toHaveStyle({ width: "100%" });
  });

  it("is keyed while transmitting", () => {
    const { rerender } = render(<PttIndicator w={96} h={28} transmitting={false} showLabel />);
    expect(screen.getByRole("button").className).not.toContain("keyed");
    rerender(<PttIndicator w={96} h={28} transmitting showLabel />);
    expect(screen.getByRole("button").className).toContain("keyed");
    expect(screen.getByRole("button")).toHaveTextContent("TRANSMIT");
  });

  it("drops the text but keeps an accessible name when the descriptor has no room", () => {
    render(<PttIndicator w={32} h={30} transmitting={false} showLabel={false} />);
    const b = screen.getByRole("button");
    expect(b).not.toHaveTextContent("PUSH-TO-TALK");
    expect(b).toHaveAccessibleName(/push-to-talk/i);
  });
});
