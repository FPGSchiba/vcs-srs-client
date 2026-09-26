import { describe, it, expect, vi } from "vitest";
import { StrictMode, useState } from "react";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";

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
  // Awaited, not synchronous: the unmount cancel is deferred by one
  // microtask so StrictMode's simulated unmount can veto it (see the
  // StrictMode block at the bottom of this file). The CONTRACT is
  // unchanged -- a real unmount while listening still cancels -- only the
  // tick it lands on moved.
  it("cancels on unmount while listening", async () => {
    const onCapture = vi.fn();
    const onCancel = vi.fn();
    const { unmount } = render(
      <KeyChip binding="" onCapture={onCapture} onCancel={onCancel} />,
    );
    fireEvent.click(screen.getByText("—"));
    unmount();
    await waitFor(() => expect(onCancel).toHaveBeenCalled());
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

/**
 * Both window roots render inside `React.StrictMode` (frontend/src/main.tsx
 * and comms.tsx), which in development runs every effect as
 * setup -> cleanup -> setup on the same element. The suite above renders
 * bare, so it never exercised that, and a real defect hid behind the gap:
 * the unmount-cancel fired on StrictMode's SIMULATED unmount and killed
 * every capture the instant it began. These tests render the way the app
 * actually does.
 */
describe("KeyChip under StrictMode", () => {
  /** Mirrors Keybinds.tsx: a "+" button type-swaps into an autoListen chip. */
  function Row({ onCancel }: { onCancel: () => void }) {
    const [capturing, setCapturing] = useState(false);
    return capturing ? (
      <KeyChip
        binding=""
        autoListen
        onCapture={() => {}}
        onCancel={() => {
          onCancel();
          setCapturing(false);
        }}
      />
    ) : (
      <button type="button" onClick={() => setCapturing(true)}>
        +
      </button>
    );
  }

  it("keeps listening after the capture affordance is clicked", async () => {
    const onCancel = vi.fn();
    render(
      <StrictMode>
        <Row onCancel={onCancel} />
      </StrictMode>,
    );
    fireEvent.click(screen.getByText("+"));
    // Awaited: the cancel this asserts against is deferred by a microtask,
    // so a synchronous assertion would pass even with the bug present.
    await waitFor(() => expect(screen.queryByText("PRESS\u2026")).toBeInTheDocument());
    expect(onCancel).not.toHaveBeenCalled();
  });

  it("still delivers the captured key", async () => {
    const onCapture = vi.fn();
    render(
      <StrictMode>
        <KeyChip binding="" autoListen onCapture={onCapture} onCancel={() => {}} />
      </StrictMode>,
    );
    await waitFor(() => expect(screen.queryByText("PRESS\u2026")).toBeInTheDocument());
    fireEvent.keyDown(window, { code: "KeyV", key: "v" });
    expect(onCapture).toHaveBeenCalledWith({
      code: "KeyV",
      ctrl: false,
      alt: false,
      shift: false,
      super: false,
    });
  });

  // The row switch the deferred cancel must NOT break: a genuine unmount
  // while listening still has to tell the backend, or clicking another row
  // leaves the first capture suspended until the backend's own timeout.
  it("still cancels on a real unmount while listening", async () => {
    const onCancel = vi.fn();
    const { unmount } = render(
      <StrictMode>
        <KeyChip binding="" autoListen onCapture={() => {}} onCancel={onCancel} />
      </StrictMode>,
    );
    unmount();
    await waitFor(() => expect(onCancel).toHaveBeenCalledTimes(1));
  });

  it("does not cancel on unmount when it was never listening", async () => {
    const onCancel = vi.fn();
    const { unmount } = render(
      <StrictMode>
        <KeyChip binding="F1" onCapture={() => {}} onCancel={onCancel} />
      </StrictMode>,
    );
    unmount();
    await new Promise((r) => queueMicrotask(() => r(null)));
    expect(onCancel).not.toHaveBeenCalled();
  });
});
