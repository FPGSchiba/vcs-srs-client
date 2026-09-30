import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { TalkerLine } from "./TalkerLine";

describe("TalkerLine", () => {
  // design/vcs/radio-variants.md §9: there is no live per-radio RX feed yet.
  it("says there is no traffic rather than inventing a talker", () => {
    render(<TalkerLine disabled={false} />);
    expect(screen.getByText("no traffic")).toBeInTheDocument();
    expect(screen.getByRole("meter")).toHaveAttribute("aria-valuenow", "0");
  });

  it("shows a receiving talker with the receive accent", () => {
    render(<TalkerLine disabled={false} talker="Dabble" level={0.5} />);
    expect(screen.getByText("Dabble")).toHaveStyle({ color: "var(--ac-ok)" });
    expect(screen.getByTestId("talker-marker")).toHaveTextContent("▶");
  });

  it("shows own transmission distinctly from someone else's", () => {
    render(<TalkerLine disabled={false} self level={0.5} />);
    expect(screen.getByText("you")).toHaveStyle({ color: "var(--ac-primary)" });
    expect(screen.getByTestId("talker-marker")).toHaveTextContent("▲");
  });

  it("prefers own transmission over an incoming talker", () => {
    render(<TalkerLine disabled={false} self talker="Dabble" level={0.5} />);
    expect(screen.getByText("you")).toBeInTheDocument();
    expect(screen.queryByText("Dabble")).toBeNull();
  });

  it("replaces the whole line with OFF when the radio is disabled", () => {
    render(<TalkerLine disabled talker="Dabble" level={0.9} />);
    expect(screen.getByText("OFF")).toBeInTheDocument();
    expect(screen.queryByText("Dabble")).toBeNull();
    expect(screen.queryByRole("meter")).toBeNull();
  });

  // Review Focus #3, at this level: a long talker name must ellipsis, not grow.
  it("truncates a long talker name rather than widening the line", () => {
    render(<TalkerLine disabled={false} talker={"A".repeat(120)} level={0.5} />);
    expect(screen.getByText("A".repeat(120))).toHaveStyle({
      overflow: "hidden",
      textOverflow: "ellipsis",
      whiteSpace: "nowrap",
    });
  });

  it("keeps the meter next to the name rather than pushing it to the edge", () => {
    // The flexible spacer must come AFTER the meter, so the group reads as one
    // thing. `flex: 1` on the name would push the meter to the far right.
    render(<TalkerLine disabled={false} talker="Dabble" level={0.5} />);
    const spacer = screen.getByTestId("talker-spacer");
    const meter = screen.getByRole("meter");
    // Verify the spacer comes after the meter in the DOM
    expect(spacer.previousElementSibling).toBe(meter);
    // Verify the name does not have flex-grow set
    const name = screen.getByText("Dabble");
    const computedStyle = window.getComputedStyle(name);
    expect(computedStyle.flexGrow).toBe("0");
  });

  it("does not show signal when there is no talker, even with high level", () => {
    // The honesty rule: meter must show zero when there is no talker.
    // This prevents stale level from being misread as incoming signal.
    render(<TalkerLine disabled={false} level={0.8} />);
    expect(screen.getByText("no traffic")).toBeInTheDocument();
    expect(screen.getByRole("meter")).toHaveAttribute("aria-valuenow", "0");
  });

  it("distinguishes own transmission from a received talker", () => {
    // The user needs to know: am I talking, or hearing someone else?
    // Render both and assert they differ in colour and marker.
    const { rerender } = render(<TalkerLine disabled={false} self level={0.5} />);
    const selfText = screen.getByText("you");
    const selfMarker = screen.getByTestId("talker-marker");
    const selfColor = selfText.style.color;
    const selfMarkerText = selfMarker.textContent;

    rerender(<TalkerLine disabled={false} talker="Dabble" level={0.5} />);
    const talkingText = screen.getByText("Dabble");
    const talkingMarker = screen.getByTestId("talker-marker");
    const talkingColor = talkingText.style.color;
    const talkingMarkerText = talkingMarker.textContent;

    expect(selfColor).not.toBe(talkingColor);
    expect(selfMarkerText).not.toBe(talkingMarkerText);
  });
});
