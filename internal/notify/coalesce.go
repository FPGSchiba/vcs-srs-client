package notify

import "time"

// pendingChange is one key's coalescing state. Filled in by Task 4.
type pendingChange struct {
	timer     *time.Timer
	windowEnd time.Time
}
