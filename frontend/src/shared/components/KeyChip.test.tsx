import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";

import { KeyChip } from "./KeyChip";

describe("KeyChip", () => {
  it("renders the bound chord", () => {
    render(<KeyChip binding="Ctrl+E" onCapture={() => {}} />);
    expect(screen.getByText("Ctrl")).toBeInTheDocument();
    expect(screen.getByText("E")).toBeInTheDocument();
  });

  it("shows an em dash when unbound", () => {
    render(<KeyChip binding="" onCapture={() => {}} />);
    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("enters listening state on click", () => {
    render(<KeyChip binding="" onCapture={() => {}} />);
    fireEvent.click(screen.getByText("—"));
    expect(screen.getByText("PRESS…")).toBeInTheDocument();
  });

  it("reports the physical code and modifiers, not the layout key", () => {
    const onCapture = vi.fn();
    render(<KeyChip binding="" onCapture={onCapture} />);
    fireEvent.click(screen.getByText("—"));
    fireEvent.keyDown(window, { code: "Digit1", key: "!", altKey: true });
    expect(onCapture).toHaveBeenCalledWith({
      code: "Digit1", ctrl: false, alt: true, shift: false, super: false,
    });
  });

  it("cancels on Escape without capturing", () => {
    const onCapture = vi.fn();
    const onCancel = vi.fn();
    render(<KeyChip binding="F1" onCapture={onCapture} onCancel={onCancel} />);
    fireEvent.click(screen.getByText("F1"));
    fireEvent.keyDown(window, { code: "Escape", key: "Escape" });
    expect(onCapture).not.toHaveBeenCalled();
    expect(onCancel).toHaveBeenCalled();
  });

  it("ignores bare modifier presses while listening", () => {
    const onCapture = vi.fn();
    render(<KeyChip binding="" onCapture={onCapture} />);
    fireEvent.click(screen.getByText("—"));
    fireEvent.keyDown(window, { code: "ShiftLeft", key: "Shift", shiftKey: true });
    expect(onCapture).not.toHaveBeenCalled();
    expect(screen.getByText("PRESS…")).toBeInTheDocument();
  });

  // Not part of the brief's pinned test list, but the behaviour it covers is
  // called out explicitly: the backend suspends all hotkeys while listening,
  // so a chip that unmounts mid-capture MUST cancel or every hotkey stays
  // dead until the server-side timeout.
  it("cancels on unmount while listening", () => {
    const onCapture = vi.fn();
    const onCancel = vi.fn();
    const { unmount } = render(
      <KeyChip binding="" onCapture={onCapture} onCancel={onCancel} />,
    );
    fireEvent.click(screen.getByText("—"));
    unmount();
    expect(onCancel).toHaveBeenCalled();
    expect(onCapture).not.toHaveBeenCalled();
  });

  // Design spec §7 cancel path: re-clicking a listening chip backs out of
  // the capture without pressing a key. Same "backend stays suspended"
  // failure mode as an uncancelled unmount if this doesn't fire onCancel.
  it("cancels when the chip is clicked again while listening", () => {
    const onCapture = vi.fn();
    const onCancel = vi.fn();
    render(<KeyChip binding="" onCapture={onCapture} onCancel={onCancel} />);
    fireEvent.click(screen.getByText("—"));
    fireEvent.click(screen.getByText("PRESS…"));
    expect(onCancel).toHaveBeenCalled();
    expect(onCapture).not.toHaveBeenCalled();
    expect(screen.queryByText("PRESS…")).not.toBeInTheDocument();
  });

  // Design spec §7 cancel path: alt-tabbing away mid-capture must not leave
  // the backend suspended until the 10s timeout.
  it("cancels when the window loses focus while listening", () => {
    const onCapture = vi.fn();
    const onCancel = vi.fn();
    render(<KeyChip binding="" onCapture={onCapture} onCancel={onCancel} />);
    fireEvent.click(screen.getByText("—"));
    fireEvent.blur(window);
    expect(onCancel).toHaveBeenCalled();
    expect(onCapture).not.toHaveBeenCalled();
    expect(screen.queryByText("PRESS…")).not.toBeInTheDocument();
  });
});
