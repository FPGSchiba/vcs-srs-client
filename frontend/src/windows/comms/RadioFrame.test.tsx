import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
// tokens.css is the in-bundle mirror of design/vcs/project/styles.css (:root block);
// vite will not serve files outside the frontend root.
import css from "../../shared/styles/tokens.css?raw";
import src from "./RadioFrame.tsx?raw";
import { RadioFrame, frameAccent, type RadioState } from "./RadioFrame";

const base: RadioState = {
  selected: false,
  receiving: false,
  transmitting: false,
  intercom: false,
  disabled: false,
};

function show(s: Partial<RadioState> = {}, onSelect = vi.fn()) {
  cleanup(); // several tests call show() more than once; keep exactly one option in the DOM
  render(
    <RadioFrame {...base} {...s} w={280} h={166} label="R01 Fleet Common" onSelect={onSelect}>
      <span>body</span>
    </RadioFrame>,
  );
  return screen.getByRole("option");
}

describe("state attributes", () => {
  it("is plain by default", () => {
    const f = show();
    for (const a of ["data-selected", "data-intercom", "data-disabled", "data-tx", "data-rx"]) {
      expect(f).toHaveAttribute(a, "false");
    }
  });

  it("marks each state independently", () => {
    expect(show({ selected: true })).toHaveAttribute("data-selected", "true");
    expect(show({ intercom: true })).toHaveAttribute("data-intercom", "true");
    expect(show({ disabled: true })).toHaveAttribute("data-disabled", "true");
    expect(show({ transmitting: true })).toHaveAttribute("data-tx", "true");
    expect(show({ receiving: true })).toHaveAttribute("data-rx", "true");
  });
});

describe("the accent", () => {
  it("uses different colour channels for selection and intercom, so both can show", () => {
    expect(frameAccent({ ...base, selected: true })).toBe("var(--ac-primary)");
    expect(frameAccent({ ...base, intercom: true })).toBe("var(--ac-warn)");
    // A selected intercom radio keeps its blue border AND its amber brackets;
    // the two never compete for the same pixel.
    expect(frameAccent({ ...base, selected: true, intercom: true })).toBe("var(--ac-warn)");
  });

  it("drains to a border token when disabled", () => {
    expect(frameAccent({ ...base, disabled: true, selected: true })).toBe("var(--bd-1)");
  });

  it("gives transmitting alone the primary accent", () => {
    expect(frameAccent({ ...base, transmitting: true })).toBe("var(--ac-primary)");
  });

  it("falls back to the default border token when nothing is active", () => {
    expect(frameAccent(base)).toBe("var(--bd-2)");
  });

  it("lets intercom outrank transmitting", () => {
    expect(frameAccent({ ...base, intercom: true, transmitting: true })).toBe("var(--ac-warn)");
  });

  it("lets disabled outrank intercom", () => {
    expect(frameAccent({ ...base, disabled: true, intercom: true })).toBe("var(--bd-1)");
  });

  it("only uses tokens the stylesheet defines", () => {
    const used = new Set([...src.matchAll(/var\((--[\w-]+)/g)].map((m) => m[1]));
    expect(used.size).toBeGreaterThan(0);
    for (const name of used) expect(css).toContain(`${name}:`);
  });
});

describe("disabled border", () => {
  it("keeps a visible selection cue on a selected disabled radio", () => {
    const selectedBorder = show({ disabled: true, selected: true }).style.border;
    const plainBorder = show({ disabled: true }).style.border;
    expect(selectedBorder).not.toBe("");
    expect(plainBorder).not.toBe("");
    expect(selectedBorder).not.toBe(plainBorder);
    expect(selectedBorder).toContain("--ac-primary");
    expect(plainBorder).not.toContain("--ac-primary");
  });
});

describe("intercom and disabled treatments", () => {
  it("draws an amber left edge for intercom", () => {
    expect(show({ intercom: true }).querySelector("[data-intercom-edge]")).not.toBeNull();
    expect(show().querySelector("[data-intercom-edge]")).toBeNull();
  });

  it("draws a hatch overlay when disabled", () => {
    expect(show({ disabled: true }).querySelector("[data-disabled-hatch]")).not.toBeNull();
    expect(show().querySelector("[data-disabled-hatch]")).toBeNull();
  });

  it("does not let the hatch swallow clicks", () => {
    expect(show({ disabled: true }).querySelector("[data-disabled-hatch]")).toHaveStyle({
      pointerEvents: "none",
    });
  });

  it("draws three corner brackets", () => {
    expect(show().querySelectorAll("[data-bracket]")).toHaveLength(3);
  });
});

describe("selection", () => {
  it("is an option carrying its own selected state and accessible name", () => {
    const f = show({ selected: true });
    expect(f).toHaveAttribute("aria-selected", "true");
    expect(f).toHaveAccessibleName("R01 Fleet Common");
  });

  it("selects on click", () => {
    const onSelect = vi.fn();
    fireEvent.click(show({}, onSelect));
    expect(onSelect).toHaveBeenCalledTimes(1);
  });

  it("selects on Enter and Space from the frame itself", () => {
    const onSelect = vi.fn();
    const f = show({}, onSelect);
    fireEvent.keyDown(f, { key: "Enter" });
    fireEvent.keyDown(f, { key: " " });
    expect(onSelect).toHaveBeenCalledTimes(2);
  });

  it("ignores a key bubbled from a child control", () => {
    const onSelect = vi.fn();
    show({}, onSelect);
    fireEvent.keyDown(screen.getByText("body"), { key: " " });
    expect(onSelect).not.toHaveBeenCalled();
  });

  it("is still selectable when the radio is disabled", () => {
    // "Disabled" is the RADIO's enabled flag, not the card's interactivity:
    // a disabled radio must remain selectable and keep a visible selection.
    const onSelect = vi.fn();
    fireEvent.click(show({ disabled: true }, onSelect));
    expect(onSelect).toHaveBeenCalledTimes(1);
  });
});

describe("size", () => {
  it("takes its box from the variant", () => {
    expect(show()).toHaveStyle({ width: "280px", height: "166px" });
  });
});
