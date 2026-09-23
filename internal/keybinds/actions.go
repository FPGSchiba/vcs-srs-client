// Package keybinds owns the canonical action registry and the action->chord
// binding store. It is pure in-memory: it does not touch the filesystem and does
// not import internal/config. Callers persist Snapshot() wherever they like.
package keybinds

import (
	"fmt"

	"github.com/FPGSchiba/vcs-srs-client/internal/chord"
	"github.com/FPGSchiba/vcs-srs-client/internal/trigger"
)

// ActionID identifies a bindable action, e.g. "global.ptt", "radio.1.ptt".
type ActionID string

// Kind says whether an action needs key release as well as key press.
type Kind uint8

const (
	// KindPress fires once on key down.
	KindPress Kind = iota
	// KindHold fires on key down AND key up (push-to-talk semantics).
	KindHold
)

// Category groups actions for display.
type Category uint8

const (
	CatGlobal Category = iota
	CatChannel
	CatPerRadio
	CatStatus
)

// Action is a bindable action's static metadata.
type Action struct {
	ID       ActionID
	Label    string
	Desc     string
	Category Category
	Kind     Kind
}

// StaticActions returns every action that is not derived from a radio.
func StaticActions() []Action {
	return []Action{
		{"global.ptt", "Global PTT", "Transmits on the currently Selected radio", CatGlobal, KindHold},
		{"global.push_to_mute", "Push-to-mute", "", CatGlobal, KindHold},
		{"global.mute_toggle", "Mute toggle", "", CatGlobal, KindPress},
		{"global.emergency_broadcast", "Emergency broadcast", "", CatGlobal, KindPress},
		{"global.compact_overlay", "Open compact overlay", "", CatGlobal, KindPress},

		{"channel.intercom", "Intercom", "", CatChannel, KindPress},
		{"channel.role", "Role channel", "", CatChannel, KindPress},
		{"channel.ship", "Ship-wide", "", CatChannel, KindPress},
		{"channel.fleet", "Fleet-wide", "", CatChannel, KindPress},

		{"status.available", "Set status: Available", "", CatStatus, KindPress},
		{"status.combat", "Set status: In Combat", "", CatStatus, KindPress},
		{"status.discipline", "Set status: Comms Discipline", "", CatStatus, KindPress},
		{"status.afk", "Set status: AFK", "", CatStatus, KindPress},
	}
}

// RadioRef is the minimal radio identity the registry needs.
type RadioRef struct {
	ID   uint32
	Name string
}

// PerRadioActions derives the PTT and Select actions for the given radios.
func PerRadioActions(radios []RadioRef) []Action {
	out := make([]Action, 0, len(radios)*2)
	for _, r := range radios {
		label := fmt.Sprintf("R%02d · %s", r.ID, r.Name)
		out = append(out,
			Action{ActionID(fmt.Sprintf("radio.%d.ptt", r.ID)), label + " (PTT)", "", CatPerRadio, KindHold},
			Action{ActionID(fmt.Sprintf("radio.%d.select", r.ID)), label + " (Select)", "", CatPerRadio, KindPress},
		)
	}
	return out
}

// Defaults are the bindings shipped on first run, matching the design
// prototype. global.ptt ships unbound deliberately -- it is the one the user
// is most likely to want on their own key. No joystick defaults ship: we
// cannot know what devices a user owns.
func Defaults() map[ActionID][]trigger.Trigger {
	must := func(s string) []trigger.Trigger {
		c, err := chord.Parse(s)
		if err != nil {
			panic("keybinds: bad default chord " + s + ": " + err.Error())
		}
		return []trigger.Trigger{trigger.Key(c)}
	}
	return map[ActionID][]trigger.Trigger{
		"global.push_to_mute":        must("V"),
		"global.mute_toggle":         must("M"),
		"global.emergency_broadcast": must("Ctrl+E"),
		"global.compact_overlay":     must("Ctrl+O"),
		"channel.intercom":           must("1"),
		"channel.role":               must("2"),
		"channel.ship":               must("3"),
		"channel.fleet":              must("4"),
		"status.available":           must("Alt+1"),
		"status.combat":              must("Alt+2"),
		"status.discipline":          must("Alt+3"),
		"status.afk":                 must("Alt+4"),
	}
}
