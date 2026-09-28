package app

import (
	"sort"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/keybinds"
	"github.com/FPGSchiba/vcs-srs-client/internal/notify"
)

// notifyCategory is the only category Phase 7.2 emits. The full seven-value
// table lives in the frontend (shared/components/notificationCategories.ts);
// the backend only ever needs to name one.
const notifyCategory = "system"

// setNotifyWindows overrides the joystick and audio coalescing windows. For
// tests only, and UNEXPORTED for the same reason setCaptureTimeout is: an
// exported method on the service is bound and reachable from the webview,
// and a coalescing window is not something the frontend has any business
// setting. Non-positive values leave the corresponding default in place.
//
// There is deliberately no override for the hotkey window: notify.WindowHotkeys
// is already 0 (immediate, no timer), so there is nothing to wait for and a
// zero-means-default sentinel could not express it anyway.
func (a *App) setNotifyWindows(joystick, audio time.Duration) {
	if joystick > 0 {
		a.notifWinJoystick = joystick
	}
	if audio > 0 {
		a.notifWinAudio = audio
	}
}

// joystickWindow returns the injected override when one is set, else
// notify.WindowJoystick.
func (a *App) joystickWindow() time.Duration {
	if a.notifWinJoystick > 0 {
		return a.notifWinJoystick
	}
	return notify.WindowJoystick
}

// audioWindow returns the injected override when one is set, else
// notify.WindowAudio.
func (a *App) audioWindow() time.Duration {
	if a.notifWinAudio > 0 {
		return a.notifWinAudio
	}
	return notify.WindowAudio
}

// notifyHotkeyState translates one HotkeyStateDTO into notifications.
//
// Two of the ROADMAP's three required cases, and they must stay distinct:
//
//   - A GLOBAL failure (Registered == false) notifies ONCE, carrying the
//     permission state so the UI branches on a state rather than on the
//     registrar's error text.
//   - A PER-BINDING failure notifies per action, NAMING the action -- but
//     only while Registered is true. Registered == false implies Failed
//     names every bound action, so raising them alongside the global item
//     would reprint one message nineteen times. This mirrors the rule
//     Keybinds.tsx:332 already enforces for the inline per-row text.
//
// Called from emitHotkeyState, which is the sole caller of
// events.Tagged.HotkeysState -- so no second path can bypass the channel.
func (a *App) notifyHotkeyState(dto HotkeyStateDTO) {
	n := a.notif
	if n == nil {
		return
	}

	if dto.Registered {
		n.ResolveWindowed(keyHotkeysGlobal, notify.WindowHotkeys)
	} else {
		n.RaiseWindowed(keyHotkeysGlobal, notify.Item{
			Category: notifyCategory,
			Severity: notify.SeverityError,
			Icon:     "bolt",
			Title:    "Global hotkeys unavailable",
			Body:     dto.Error,
			Context:  []notify.KV{{Key: "PERMISSION", Value: dto.Permission}},
			Actions: []notify.Action{{
				Label:   "OPEN KEYBIND SETTINGS",
				Icon:    "settings",
				Kind:    "navigate",
				Target:  "settings",
				Primary: true,
			}},
		}, notify.WindowHotkeys)
	}

	// Per-binding rows. Sorted so the raise order is deterministic, which
	// makes the resulting list order stable across runs and testable.
	failed := dto.Failed
	if !dto.Registered {
		failed = nil // suppressed; the global item covers it
	}
	ids := make([]string, 0, len(failed))
	for id := range failed {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		n.RaiseWindowed(bindingKey(id), notify.Item{
			Category: notifyCategory,
			Severity: notify.SeverityWarn,
			Icon:     "bolt",
			Title:    "Binding not registered · " + a.labelFor(keybinds.ActionID(id)),
			Body:     failed[id],
			Actions: []notify.Action{{
				Label:  "OPEN KEYBIND SETTINGS",
				Icon:   "settings",
				Kind:   "navigate",
				Target: "settings",
			}},
		}, notify.WindowHotkeys)
	}

	// Resolve any per-binding item whose action is no longer failing. The
	// previous snapshot is the notifier's own list, so this needs no extra
	// bookkeeping here.
	for _, it := range n.Snapshot().Items {
		if it.Resolved || !isBindingKey(it.Key) {
			continue
		}
		if _, still := failed[actionIDFromBindingKey(it.Key)]; !still {
			n.ResolveWindowed(it.Key, notify.WindowHotkeys)
		}
	}
}

// notifyJoystickState translates one JoystickStateDTO into notifications.
//
// The third required case, and the one with a trap: "unsupported" is
// INFORMATIONAL, never a failure. macOS reports it because Star Citizen has
// no macOS build, so there is nothing for the user to grant and no action to
// offer. Info severity is raised already-read by the store, so it reaches
// neither the badge, the bell nor a toast -- the rule is enforced at the
// delivery layer rather than in the colour of a border.
//
// Called from emitJoystickState, the sole caller of
// events.Tagged.JoystickState.
func (a *App) notifyJoystickState(dto JoystickStateDTO) {
	n := a.notif
	if n == nil {
		return
	}

	window := a.joystickWindow()
	switch {
	case !dto.Supported:
		n.RaiseWindowed(keyJoystickGlobal, notify.Item{
			Category: notifyCategory,
			Severity: notify.SeverityInfo,
			Icon:     "knob",
			Title:    "Joystick input is unsupported on this platform",
			Body:     "Joystick and gamepad bindings are available on Windows and Linux only.",
		}, window)
	case dto.Error != "":
		n.RaiseWindowed(keyJoystickGlobal, notify.Item{
			Category: notifyCategory,
			Severity: notify.SeverityWarn,
			Icon:     "knob",
			Title:    "Joystick unavailable",
			Body:     dto.Error,
			Actions: []notify.Action{{
				Label:  "OPEN KEYBIND SETTINGS",
				Icon:   "settings",
				Kind:   "navigate",
				Target: "settings",
			}},
		}, window)
	default:
		n.ResolveWindowed(keyJoystickGlobal, window)
	}
}

// Notification keys. Stable strings, because dedupe, resolution and
// dismissal suppression are all keyed on them.
const (
	keyHotkeysGlobal  = "hotkeys.global"
	keyJoystickGlobal = "joystick.global"
	bindingKeyPrefix  = "hotkeys.binding."
)

func bindingKey(actionID string) string { return bindingKeyPrefix + actionID }

func isBindingKey(key string) bool {
	return len(key) > len(bindingKeyPrefix) && key[:len(bindingKeyPrefix)] == bindingKeyPrefix
}

func actionIDFromBindingKey(key string) string {
	if !isBindingKey(key) {
		return ""
	}
	return key[len(bindingKeyPrefix):]
}
