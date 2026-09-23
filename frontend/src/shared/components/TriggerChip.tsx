import { Fragment } from "react";
import type { Trigger } from "../store/settings";

interface TriggerChipProps {
  trigger: Trigger;
  onRemove: () => void;
}

/**
 * TriggerChip renders one bound trigger plus its remove affordance.
 *
 * Unlike KeyChip, this chip does NOT capture. Capture moved to a single `+`
 * per row that accepts either a keyboard chord or a joystick button, so a
 * chip is purely a display of something already bound.
 *
 * A keyboard trigger renders as `.kbd` spans joined by `.plus`, matching the
 * design prototype. A joystick trigger renders as one `.kbd` carrying the
 * backend-rendered label ("Btn 12", "Hat 1 ↑", "Btn 5 + Btn 3").
 *
 * A trigger whose device is absent renders MUTED rather than disappearing:
 * the binding is still valid and starts working again the moment the stick is
 * plugged back in, so hiding it would misrepresent the saved configuration.
 * The `disconnected` class lands on both the outer chip (so the title -- the
 * only place the device name is spelled out -- and the muted styling sit on
 * the same element) and the inner `.kbd` (so the token drives the label's own
 * dimmed color).
 */
export function TriggerChip({ trigger, onRemove }: TriggerChipProps) {
  const disconnected = trigger.kind === "joy" && !trigger.connected;

  const title =
    trigger.kind === "joy"
      ? `${trigger.device_name}${disconnected ? " — not connected" : ""}`
      : undefined;

  const body =
    trigger.kind === "key" ? (
      trigger.label.split("+").map((part, i) => (
        <Fragment key={`${part}-${i}`}>
          {i > 0 && <span className="plus">+</span>}
          <span className="kbd">{part}</span>
        </Fragment>
      ))
    ) : (
      <span className={`kbd${disconnected ? " disconnected" : ""}`}>{trigger.label}</span>
    );

  return (
    <span className={`trigger-chip${disconnected ? " disconnected" : ""}`} title={title}>
      {body}
      <button
        type="button"
        className="trigger-remove"
        aria-label={`Remove ${trigger.label}`}
        onClick={onRemove}
      >
        ×
      </button>
    </span>
  );
}
