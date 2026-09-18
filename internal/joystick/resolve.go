package joystick

import "github.com/FPGSchiba/vcs-srs-client/internal/trigger"

// Binding is one registerable joystick trigger. Hold means the action needs
// release as well as press (push-to-talk semantics), matching
// hotkeys.Binding.Hold.
type Binding struct {
	Joy  trigger.JoyBinding
	Hold bool
}

// Active returns the set of action IDs that should currently be held.
//
// An action is held when ANY of its triggers is effectively active, so the
// caller sees one boolean per action and never has to reason about which
// trigger won.
//
// SPECIFICITY. Keyboard chords get this for free -- internal/hotkeys matches
// modifiers exactly, so "E" simply does not match a Ctrl+E event. Joystick
// modifiers are arbitrary buttons, so "no modifier held" is not knowable
// without knowing which buttons are modifiers, and the rule has to be
// explicit:
//
//	global.ptt  = Btn3
//	radio.1.ptt = Btn5 + Btn3
//
// Holding Btn5+Btn3 must fire radio.1.ptt and NOT global.ptt. So a
// modifier-less trigger is suppressed whenever another ACTIVE trigger uses
// that same button as either its main input or its modifier.
func Active(binds map[string][]Binding, s State) map[string]bool {
	type sat struct {
		actionID string
		joy      trigger.JoyBinding
	}

	// Pass 1: everything whose buttons are all held.
	var satisfied []sat
	for id, list := range binds {
		for _, b := range list {
			if !s.IsHeld(b.Joy.Main()) {
				continue
			}
			if b.Joy.Modifier != nil && !s.IsHeld(*b.Joy.Modifier) {
				continue
			}
			satisfied = append(satisfied, sat{actionID: id, joy: b.Joy})
		}
	}

	// Pass 2: collect the buttons claimed by an active MODIFIER binding.
	// A bare binding on any of these loses to the more specific combination.
	claimed := map[trigger.JoyButton]bool{}
	for _, t := range satisfied {
		if t.joy.Modifier == nil {
			continue
		}
		claimed[t.joy.Main()] = true
		claimed[*t.joy.Modifier] = true
	}

	// Pass 3: drop suppressed bare bindings, fold the rest to action IDs.
	out := map[string]bool{}
	for _, t := range satisfied {
		if t.joy.Modifier == nil && claimed[t.joy.Main()] {
			continue // a more specific active binding owns this button
		}
		out[t.actionID] = true
	}
	return out
}
