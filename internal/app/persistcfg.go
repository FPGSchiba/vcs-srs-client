package app

import "github.com/FPGSchiba/vcs-srs-client/internal/config"

// cfgTxn is one config.toml write.
//
// mutate must only ASSIGN fields of the Config it is given (never write into
// a map or slice shared with sb.cfg), because it runs twice: once on the
// snapshot that is saved, and again on the then-current config at commit,
// so a concurrent in-memory edit (SetCommsLayout, captureCommsWindowSize)
// that landed while the file was being written is not lost. Values derived
// from other state (kb.Snapshot(), DTOs) are computed BEFORE persistCfg and
// captured by the closure.
type cfgTxn struct {
	// check, if set, runs under sb.mu before the snapshot; false aborts with
	// no save and persistCfg returns (false, nil).
	check func(cur *config.Config) bool
	// mutate applies the change. See the type doc for its purity rule.
	mutate func(c *config.Config)
	// onCommit, if set, runs under sb.mu after a successful save, once the
	// change is applied to sb.cfg.
	onCommit func()
}

// persistCfg snapshots sb.cfg under sb.mu, saves the mutated snapshot with
// sb.mu RELEASED, then re-applies the mutation onto the current config
// under sb.mu. saveMu (taken first, lock order writeMu -> saveMu -> mu)
// serialises writers so the file always ends with the newest snapshot.
//
// On a failed save sb.cfg is left untouched, so in-memory config never
// claims a write that is not on disk. A "" cfgPath (tests) skips the disk
// write but still commits. The caller must not hold sb.mu, and nothing here
// emits an event.
func (sb *settingsBackend) persistCfg(tx cfgTxn) (committed bool, err error) {
	sb.saveMu.Lock()
	defer sb.saveMu.Unlock()

	sb.mu.Lock()
	if tx.check != nil && !tx.check(sb.cfg) {
		sb.mu.Unlock()
		return false, nil
	}
	next := *sb.cfg
	tx.mutate(&next)
	path := sb.cfgPath
	save := sb.save
	sb.mu.Unlock()

	if save == nil {
		save = config.Save
	}
	if path != "" {
		if err := save(path, &next); err != nil {
			return false, err
		}
	}

	sb.mu.Lock()
	cur := *sb.cfg
	tx.mutate(&cur)
	sb.cfg = &cur
	if tx.onCommit != nil {
		tx.onCommit()
	}
	sb.mu.Unlock()
	return true, nil
}
