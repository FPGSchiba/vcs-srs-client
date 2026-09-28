import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";

import { NotifRow } from "./NotifRow";
import type { NotifyItem } from "../../shared/store/notifications";

const item = (over: Partial<NotifyItem> = {}): NotifyItem => ({
  id: "n1",
  key: "hotkeys.global",
  category: "system",
  severity: "error",
  icon: "bolt",
  title: "Global hotkeys unavailable",
  body: "no backend",
  context: [{ key: "PERMISSION", value: "denied" }],
  actions: [],
  time: "2026-09-28T12:00:00Z",
  unread: true,
  resolved: false,
  ...over,
});

describe("NotifRow", () => {
  it("renders the title, category label and unread dot", () => {
    const { container } = render(
      <NotifRow item={item()} expanded={false} onToggle={() => {}} onAction={() => {}} />,
    );

    expect(screen.getByText("Global hotkeys unavailable")).toBeInTheDocument();
    expect(screen.getByText("SYSTEM")).toBeInTheDocument();
    expect(container.querySelector("[data-unread]")).not.toBeNull();
  });

  it("hides the body and context until expanded", () => {
    const { rerender } = render(
      <NotifRow item={item()} expanded={false} onToggle={() => {}} onAction={() => {}} />,
    );
    expect(screen.queryByText("no backend")).toBeNull();

    rerender(<NotifRow item={item()} expanded onToggle={() => {}} onAction={() => {}} />);
    expect(screen.getByText("no backend")).toBeInTheDocument();
    expect(screen.getByText("PERMISSION")).toBeInTheDocument();
    expect(screen.getByText("denied")).toBeInTheDocument();
  });

  it("activates from a click and from the keyboard", () => {
    const onToggle = vi.fn();
    const { container } = render(
      <NotifRow item={item()} expanded={false} onToggle={onToggle} onAction={() => {}} />,
    );
    const header = container.querySelector('[role="button"]') as HTMLElement;

    // typescript:S1082 -- a clickable non-button needs a keyboard path.
    expect(header).not.toBeNull();
    expect(header.getAttribute("tabindex")).toBe("0");

    fireEvent.click(header);
    fireEvent.keyDown(header, { key: "Enter" });
    fireEvent.keyDown(header, { key: " " });
    expect(onToggle).toHaveBeenCalledTimes(3);
  });

  it("renders actions only when expanded and reports them by kind and target", () => {
    const onAction = vi.fn();
    const withAction = item({
      actions: [
        { label: "OPEN KEYBIND SETTINGS", icon: "settings", kind: "navigate", target: "settings", primary: true },
      ],
    });

    const { rerender } = render(
      <NotifRow item={withAction} expanded={false} onToggle={() => {}} onAction={onAction} />,
    );
    expect(screen.queryByText("OPEN KEYBIND SETTINGS")).toBeNull();

    rerender(<NotifRow item={withAction} expanded onToggle={() => {}} onAction={onAction} />);
    fireEvent.click(screen.getByText("OPEN KEYBIND SETTINGS"));

    expect(onAction).toHaveBeenCalledWith(withAction.actions[0]);
  });

  it("marks a resolved item and drops its unread dot", () => {
    const { container } = render(
      <NotifRow
        item={item({ resolved: true, unread: false })}
        expanded={false}
        onToggle={() => {}}
        onAction={() => {}}
      />,
    );

    expect(container.querySelector("[data-resolved]")).not.toBeNull();
    expect(container.querySelector("[data-unread]")).toBeNull();
  });

  it("does not blow out its container on a long body, and does not interpret markup", () => {
    // Device names and OS error strings flow straight into title/body. A
    // 400-character malgo error must wrap rather than stretch the row, and
    // an error containing angle brackets must render as text.
    const long = "x".repeat(400);
    const { container } = render(
      <NotifRow
        item={item({ body: `${long} <img src=x onerror="boom">` })}
        expanded
        onToggle={() => {}}
        onAction={() => {}}
      />,
    );

    expect(container.querySelector("img")).toBeNull();
    const body = container.querySelector("[data-body]") as HTMLElement;
    expect(body).not.toBeNull();
    expect(body.style.overflowWrap).toBe("anywhere");
  });
});
