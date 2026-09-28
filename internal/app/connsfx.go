package app

import (
	"github.com/FPGSchiba/vcs-srs-client/internal/connhealth"
)

// connectionSFXID maps a control-link transition to the SFX slot that marks
// it, or "" for a transition that gets no sound.
//
// Gated on general.play_connection_sounds, which has been persisted and
// exposed in Settings since Phase 3 with no consumer at all. This is its
// first one.
//
// Reconnecting deliberately gets nothing: it is a transient the user already
// sees in the status bar's pill, and a sound on every retry rung would be
// noise during exactly the period the user is already annoyed.
func (a *App) connectionSFXID(state string) string {
	sb := a.settings
	if sb == nil || sb.cfg == nil {
		return ""
	}
	sb.mu.Lock()
	enabled := sb.cfg.General.PlayConnectionSounds
	sb.mu.Unlock()
	if !enabled {
		return ""
	}
	switch state {
	case connhealth.StateConnected:
		return "connect"
	case connhealth.StateDisconnected:
		return "disconnect"
	default:
		return ""
	}
}

// playConnectionSFX plays the sound marking a control-link transition.
//
// It ships SILENT: connect.wav and disconnect.wav do not exist, adding to
// the seven sample files already outstanding from Phase 4. The engine is
// asset-agnostic by design (Phase 4 D11) -- a missing sample is logged once
// and plays silence, never a crash -- so this wiring is correct and testable
// today and goes audible the moment the samples land.
func (a *App) playConnectionSFX(state string) {
	id := a.connectionSFXID(state)
	if id == "" {
		return
	}
	m := a.audioManager()
	if m == nil {
		return
	}
	m.PlayEffect(id)
}

// PlayConnectionSFX is playConnectionSFX's exported form, for main.go's
// control-state observer wiring.
func (a *App) PlayConnectionSFX(state string) { a.playConnectionSFX(state) }
