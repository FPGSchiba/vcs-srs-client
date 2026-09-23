import { describe, expect, it, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { TriggerChip } from "./TriggerChip";
import type { Trigger } from "../store/settings";

const keyTrigger: Trigger = {
  kind: "key",
  chord: "Ctrl+F1",
  device: "",
  device_name: "",
  label: "Ctrl+F1",
  connected: true,
};

const joyTrigger: Trigger = {
  kind: "joy",
  chord: "",
  device: "stick-c3",
  device_name: "VPC MongoosT-50CM3",
  label: "Btn 12",
  connected: true,
};

describe("TriggerChip", () => {
  it("splits a keyboard chord into one kbd per key", () => {
    render(<TriggerChip trigger={keyTrigger} onRemove={vi.fn()} />);
    expect(screen.getByText("Ctrl")).toBeInTheDocument();
    expect(screen.getByText("F1")).toBeInTheDocument();
  });

  it("renders a joystick trigger as a single labelled chip", () => {
    render(<TriggerChip trigger={joyTrigger} onRemove={vi.fn()} />);
    expect(screen.getByText("Btn 12")).toBeInTheDocument();
  });

  it("marks a disconnected device without hiding the binding", () => {
    // The binding is still valid and returns when the stick is plugged back
    // in, so it must stay visible and stay named.
    render(
      <TriggerChip trigger={{ ...joyTrigger, connected: false }} onRemove={vi.fn()} />,
    );
    const chip = screen.getByTitle(/not connected/i);
    expect(chip).toBeInTheDocument();
    expect(chip.className).toContain("disconnected");
    expect(screen.getByText("Btn 12")).toBeInTheDocument();
  });

  it("names the device in the title so identical sticks are tellable apart", () => {
    render(<TriggerChip trigger={joyTrigger} onRemove={vi.fn()} />);
    expect(screen.getByTitle(/VPC MongoosT-50CM3/)).toBeInTheDocument();
  });

  it("calls onRemove when the remove affordance is clicked", () => {
    const onRemove = vi.fn();
    render(<TriggerChip trigger={joyTrigger} onRemove={onRemove} />);
    fireEvent.click(screen.getByRole("button", { name: /remove/i }));
    expect(onRemove).toHaveBeenCalledTimes(1);
  });

  it("does not call onRemove when the chip itself is clicked", () => {
    // The chip is not a capture affordance any more -- the row's + is.
    const onRemove = vi.fn();
    render(<TriggerChip trigger={joyTrigger} onRemove={onRemove} />);
    fireEvent.click(screen.getByText("Btn 12"));
    expect(onRemove).not.toHaveBeenCalled();
  });
});
