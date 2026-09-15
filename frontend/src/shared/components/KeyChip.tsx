import { Fragment, useEffect, useRef, useState } from "react";

export interface Capture {
  code: string;
  ctrl: boolean;
  alt: boolean;
  shift: boolean;
  super: boolean;
}

interface KeyChipProps {
  binding: string;
  onCapture: (c: Capture) => void;
  onCancel?: () => void;
}

/** Physical key codes that are modifiers on their own -- ignored while
 * listening so the chip keeps waiting for the actual key of the chord. */
const BARE_MODIFIER_CODES = new Set([
  "ShiftLeft",
  "ShiftRight",
  "ControlLeft",
  "ControlRight",
  "AltLeft",
  "AltRight",
  "MetaLeft",
  "MetaRight",
]);

/**
 * KeyChip renders a bound chord as `.kbd` spans joined by `.plus` inside a
 * `.kbd-row` (or a single `.kbd.unbound` em dash when unbound), and lets the
 * user click to capture a new chord.
 *
 * Deliberately captures `KeyboardEvent.code` (the physical key), never
 * `.key`: `.key` is keyboard-layout dependent, so what the user sees here
 * would disagree with what Go's `internal/chord` registers as an OS-level
 * global hotkey against physical keys on non-US layouts. This component does
 * no key-name translation -- it hands the raw `{code, ctrl, alt, shift,
 * super}` chord to the caller.
 *
 * While listening, the backend suspends all OS hotkey registrations, so
 * every path out of the listening state -- successful capture, Escape,
 * re-clicking the chip, the window losing focus, or unmounting mid-capture
 * -- must resolve it: capture via `onCapture`, every other exit via
 * `onCancel`. Losing that guarantee leaves every global hotkey dead until a
 * 10s server-side timeout rescues it. All of those exits funnel through
 * `stopListening` below so a future new exit path can't forget to call
 * `onCancel`.
 */
export function KeyChip({ binding, onCapture, onCancel }: KeyChipProps) {
  const [listening, setListening] = useState(false);

  // Mirrors `listening` synchronously so the unmount cleanup below can read
  // the latest value without depending on an extra render having happened.
  const listeningRef = useRef(false);
  const onCaptureRef = useRef(onCapture);
  const onCancelRef = useRef(onCancel);
  onCaptureRef.current = onCapture;
  onCancelRef.current = onCancel;

  // The single place every "stop listening" path runs through. `cancelled`
  // is false only for a successful capture (the caller already invoked
  // `onCapture` itself) -- every other exit passes `true` so `onCancel`
  // fires and the backend un-suspends its hotkeys.
  const stopListening = (cancelled: boolean) => {
    listeningRef.current = false;
    setListening(false);
    if (cancelled) onCancelRef.current?.();
  };

  const startListening = () => {
    listeningRef.current = true;
    setListening(true);
  };

  useEffect(() => {
    if (!listening) return;

    const handleKeyDown = (e: KeyboardEvent) => {
      e.preventDefault();

      if (e.code === "Escape") {
        stopListening(true);
        return;
      }

      if (BARE_MODIFIER_CODES.has(e.code)) return;

      onCaptureRef.current({
        code: e.code,
        ctrl: e.ctrlKey,
        alt: e.altKey,
        shift: e.shiftKey,
        super: e.metaKey,
      });
      stopListening(false);
    };

    // Alt-tabbing away mid-capture must not leave the backend suspended
    // forever -- treat losing window focus the same as Escape.
    const handleBlur = () => stopListening(true);

    window.addEventListener("keydown", handleKeyDown);
    window.addEventListener("blur", handleBlur);
    return () => {
      window.removeEventListener("keydown", handleKeyDown);
      window.removeEventListener("blur", handleBlur);
    };
  }, [listening]);

  // Runs only on true unmount (empty deps): if the chip is torn down mid
  // capture, tell the caller so it can un-suspend the backend's hotkeys.
  useEffect(() => {
    return () => {
      if (listeningRef.current) {
        onCancelRef.current?.();
      }
    };
  }, []);

  // Re-clicking a listening chip is itself a cancel: the user backed out
  // without pressing a key, and the backend must be told the same as on
  // Escape or blur.
  const toggleListening = () => {
    if (listeningRef.current) {
      stopListening(true);
    } else {
      startListening();
    }
  };

  if (listening) {
    return (
      <span className="kbd listening" onClick={toggleListening}>
        PRESS…
      </span>
    );
  }

  if (!binding) {
    return (
      <span className="kbd unbound" onClick={toggleListening}>
        —
      </span>
    );
  }

  const parts = binding.split("+");
  return (
    <span className="kbd-row" onClick={toggleListening}>
      {parts.map((part, i) => (
        <Fragment key={`${part}-${i}`}>
          {i > 0 && <span className="plus">+</span>}
          <span className="kbd">{part}</span>
        </Fragment>
      ))}
    </span>
  );
}
