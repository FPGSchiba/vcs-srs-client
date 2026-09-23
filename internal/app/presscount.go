package app

import "sync"

// pressCount joins several input sources into one press/release edge per
// action.
//
// An action can be held from the keyboard and a joystick at the same time.
// Without this, releasing either source would emit HotkeyReleased while the
// other was still held -- cutting the user's push-to-talk off mid-sentence.
// So: emit on 0->1 and on 1->0, and swallow everything in between.
//
// Counts HOLD actions only. Press-kind actions never receive a Released from
// either manager, so counting them would strand the count at 1 and silently
// deaden the action after its first press. The caller does that filtering.
type pressCount struct {
	mu sync.Mutex
	n  map[string]int
}

func newPressCount() *pressCount {
	return &pressCount{n: map[string]int{}}
}

// press records a source taking the action and reports whether this is the
// transition that should be emitted (0 -> 1).
func (p *pressCount) press(actionID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.n[actionID]++
	return p.n[actionID] == 1
}

// release records a source letting go and reports whether this is the
// transition that should be emitted (1 -> 0).
//
// A release with nothing held is ignored rather than going negative: an
// unbalanced Released from a source would otherwise make the NEXT genuine
// press silent, which is exactly the class of bug this type exists to stop.
func (p *pressCount) release(actionID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.n[actionID] <= 0 {
		delete(p.n, actionID)
		return false
	}
	p.n[actionID]--
	if p.n[actionID] == 0 {
		delete(p.n, actionID)
		return true
	}
	return false
}

// forget drops any held state for actionID without emitting. Used when an
// action is rebound, so a count left behind by a missed release cannot
// deaden it for the rest of the session.
func (p *pressCount) forget(actionID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.n, actionID)
}

// reset drops every held count.
func (p *pressCount) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.n = map[string]int{}
}
