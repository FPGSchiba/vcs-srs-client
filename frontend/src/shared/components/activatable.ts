import type React from "react";

/**
 * Makes a non-button element activate like one from the keyboard.
 *
 * The design prototype uses plain `div`/`span` with `onClick` and the ported
 * CSS keys off those classNames, so the elements stay as they are and gain
 * the semantics instead: focusable, announced as a button, and activated by
 * Enter or Space like a real one.
 *
 * Also what keeps SonarCloud's `typescript:S1082` satisfied -- a click
 * handler on a non-button element with no keyboard path. Phase 6 tripped
 * that rule; every clickable non-button added or touched since uses this.
 */
export function activatable(onActivate: () => void) {
  return {
    role: "button",
    tabIndex: 0,
    onClick: onActivate,
    onKeyDown: (e: React.KeyboardEvent) => {
      if (e.key === "Enter" || e.key === " ") {
        // Space would otherwise scroll the page.
        e.preventDefault();
        onActivate();
      }
    },
  };
}
