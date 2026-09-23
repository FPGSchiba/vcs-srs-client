import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { TriggerChip } from "./TriggerChip";
import { useSettings } from "../store/settings";
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

  it("names the device VISIBLY once it is disconnected", () => {
    // Spec section 10: an absent device's trigger "renders muted, naming the
    // device", and section 3 persists [keybind_devices] so the name survives
    // the unplug. A `title` alone does not satisfy that -- it needs a hover
    // the user has no reason to attempt, and it does not exist on touch. Two
    // sticks gave two identical muted "Btn 12" chips with nothing to tell
    // them apart.
    render(
      <TriggerChip trigger={{ ...joyTrigger, connected: false }} onRemove={vi.fn()} />,
    );
    expect(screen.getByText("VPC MongoosT-50CM3")).toBeInTheDocument();
  });

  it("does not spell out the device on a connected chip", () => {
    // A connected stick is answerable by looking at the desk, and a keybind
    // row has to fit several triggers side by side.
    render(<TriggerChip trigger={joyTrigger} onRemove={vi.fn()} />);
    expect(screen.queryByText("VPC MongoosT-50CM3")).not.toBeInTheDocument();
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

describe("TriggerChip connectivity against the live device list", () => {
  // `trigger.connected` is computed once, inside GetKeybinds(), and every
  // keybinds:changed emitter is a keybind MUTATION -- none of them fires on a
  // hot-plug. So a chip bound to an absent stick stayed muted for the rest of
  // the session after the stick was plugged back in, while the binding was
  // live and firing, and the reverse on unplug. The live device list from
  // joystick:state is the only thing that moves, so the chip reads that.
  const hydrate = (devices: { id: string; name: string }[]) =>
    useSettings.setState({ joystick: { supported: true, error: "", devices } });

  beforeEach(() => {
    useSettings.setState({ joystick: { supported: false, error: "", devices: [] } });
  });

  it("mutes a chip whose device is absent from the live list", () => {
    hydrate([{ id: "other-stick", name: "Other" }]);
    // `connected: true` is the STALE baked-in value -- the live list must win.
    render(<TriggerChip trigger={joyTrigger} onRemove={vi.fn()} />);
    expect(screen.getByTitle(/not connected/i).className).toContain("disconnected");
  });

  it("names the device visibly when the live list says it is gone", () => {
    hydrate([{ id: "other-stick", name: "Other" }]);
    render(<TriggerChip trigger={joyTrigger} onRemove={vi.fn()} />);
    expect(screen.getByText("VPC MongoosT-50CM3")).toBeInTheDocument();
  });

  it("un-mutes the same chip once the device appears in the list (hot-plug)", () => {
    hydrate([]);
    const { rerender } = render(
      <TriggerChip trigger={{ ...joyTrigger, connected: false }} onRemove={vi.fn()} />,
    );
    expect(screen.getByTitle(/not connected/i)).toBeInTheDocument();

    hydrate([{ id: "stick-c3", name: "VPC MongoosT-50CM3" }]);
    rerender(<TriggerChip trigger={{ ...joyTrigger, connected: false }} onRemove={vi.fn()} />);

    expect(screen.queryByTitle(/not connected/i)).not.toBeInTheDocument();
    expect(screen.getByTitle("VPC MongoosT-50CM3").className).not.toContain("disconnected");
  });

  it("mutes the same chip again when the device leaves the list (unplug)", () => {
    hydrate([{ id: "stick-c3", name: "VPC MongoosT-50CM3" }]);
    const { rerender } = render(<TriggerChip trigger={joyTrigger} onRemove={vi.fn()} />);
    expect(screen.queryByTitle(/not connected/i)).not.toBeInTheDocument();

    hydrate([]);
    rerender(<TriggerChip trigger={joyTrigger} onRemove={vi.fn()} />);

    expect(screen.getByTitle(/not connected/i).className).toContain("disconnected");
  });

  it("falls back to trigger.connected while the device list is unhydrated", () => {
    // supported:false is both the pre-hydration default AND macOS's real
    // answer. Either way there is no live device list to consult, so the
    // backend's render-time value stands -- otherwise every joystick chip
    // would flash muted on first paint.
    render(<TriggerChip trigger={joyTrigger} onRemove={vi.fn()} />);
    expect(screen.queryByTitle(/not connected/i)).not.toBeInTheDocument();

    render(<TriggerChip trigger={{ ...joyTrigger, connected: false }} onRemove={vi.fn()} />);
    expect(screen.getByTitle(/not connected/i)).toBeInTheDocument();
  });

  it("never mutes a keyboard trigger, whatever the device list says", () => {
    hydrate([]);
    render(<TriggerChip trigger={keyTrigger} onRemove={vi.fn()} />);
    expect(screen.queryByTitle(/not connected/i)).not.toBeInTheDocument();
    expect(screen.getByText("Ctrl")).toBeInTheDocument();
  });
});
