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
 * every path out of the listening state -- capture, Escape, or unmounting
 * mid-capture -- must resolve it: capture via `onCapture`, everything else
 * via `onCancel`. Losing that guarantee leaves every global hotkey dead
 * until a 10s server-side timeout rescues it.
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

  const setListeningState = (value: boolean) => {
    listeningRef.current = value;
    setListening(value);
  };

  useEffect(() => {
    if (!listening) return;

    const handleKeyDown = (e: KeyboardEvent) => {
      e.preventDefault();

      if (e.code === "Escape") {
        setListeningState(false);
        onCancelRef.current?.();
        return;
      }

      if (BARE_MODIFIER_CODES.has(e.code)) return;

      setListeningState(false);
      onCaptureRef.current({
        code: e.code,
        ctrl: e.ctrlKey,
        alt: e.altKey,
        shift: e.shiftKey,
        super: e.metaKey,
      });
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
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

  const toggleListening = () => setListeningState(!listeningRef.current);

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
