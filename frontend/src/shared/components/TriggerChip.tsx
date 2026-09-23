import { Fragment } from "react";
import { useSettings } from "../store/settings";
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
 * The `disconnected` class lands on both the outer chip (which carries the
 * title) and the inner `.kbd`, which is where the muted styling itself lives
 * (`.kbd.disconnected`).
 *
 * A DISCONNECTED CHIP NAMES ITS DEVICE VISIBLY. Spec section 10 says an absent
 * device's trigger "renders muted, naming the device", and spec section 3
 * persists `[keybind_devices]` specifically so the name survives the stick
 * being unplugged. Putting the name in a `title` alone does not satisfy that:
 * a tooltip needs a hover the user has no reason to attempt, and it is absent
 * entirely on touch. With two sticks attached, two absent bindings rendered as
 * two identical muted "Btn 12" chips with no way to tell which stick each
 * belonged to -- which is the whole reason the name is persisted.
 *
 * The name is shown ONLY while disconnected. A connected chip's device is
 * answerable by looking at the desk, and spelling it out on every chip would
 * triple the width of a keybind row that has to fit several triggers.
 *
 * CONNECTIVITY COMES FROM THE LIVE DEVICE LIST, NOT FROM `trigger.connected`.
 * The latter is computed once, inside Go's GetKeybinds(), and every
 * keybinds:changed emitter is a keybind MUTATION -- none of them fires on a
 * hot-plug. Trusting it meant a chip bound to an absent stick stayed muted and
 * tooltipped "not connected" for the rest of the session after the stick was
 * plugged back in, while the binding was live and firing; and a chip claimed
 * connected forever after an unplug. `joystick:state` is the event that does
 * move, so the chip reads the device list it carries.
 *
 * `joystick.supported` is the discriminator between "the list is empty
 * because nothing is attached" and "the list is empty because nothing has
 * answered yet". It is false in exactly two cases -- before the first
 * getJoystickState()/joystick:state lands, and on a platform with no backend
 * at all (macOS) -- and in both there IS no live list to consult, so the
 * backend's render-time `trigger.connected` stands. Getting this backwards
 * would mute every joystick chip on first paint. Once `supported` is true the
 * list is authoritative, including when it is empty.
 */
export function TriggerChip({ trigger, onRemove }: TriggerChipProps) {
  const joystick = useSettings((s) => s.joystick);

  const disconnected =
    trigger.kind === "joy" &&
    (joystick.supported
      ? !joystick.devices.some((d) => d.id === trigger.device)
      : !trigger.connected);

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
      <>
        <span className={`kbd${disconnected ? " disconnected" : ""}`}>{trigger.label}</span>
        {disconnected && trigger.device_name && (
          <span className="trigger-device">{trigger.device_name}</span>
        )}
      </>
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
