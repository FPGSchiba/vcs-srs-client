package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/FPGSchiba/vcs-srs-client/internal/config"
	"github.com/FPGSchiba/vcs-srs-client/internal/profile"
)

// selectionFor re-validates a stored radio selection against a radio set.
//
// LOAD replaces the radio set WHOLESALE, and App.resolveTXTarget returns
// nil when no ENABLED radio matches the selected id -- which is a silently
// dead PTT: hotkey:pressed still fires, the indicator still lights, and
// every frame is dropped as DroppedNoTarget. So a selection that does not
// survive the swap falls back to the first enabled radio, or 0 when the
// profile has none (the Comms window then shows its existing "No radios"
// empty state, which is honest).
//
// This is what replaces storing selected_radio_id in the profile document:
// a runtime rule with no stored field, and so no dirty-dot noise on every
// radio.<n>.select press.
func selectionFor(radios []config.Radio, current uint32) uint32 {
	for _, r := range radios {
		if r.ID == current && r.Enabled {
			return current
		}
	}
	for _, r := range radios {
		if r.Enabled {
			return r.ID
		}
	}
	return 0
}

// profileRadiosToConfig maps the document's radios to config's.
//
// The kHz integer is carried straight across. It never passes through
// RadioDTO.Frequency (float32 MHz) on this path: the server compares
// advertised frequencies with exact float32 equality, and a needless
// round trip through a float is exactly how a radio silently stops being
// relayed.
func profileRadiosToConfig(in []profile.Radio) []config.Radio {
	out := make([]config.Radio, 0, len(in))
	for _, r := range in {
		out = append(out, config.Radio{
			ID:           r.ID,
			Name:         r.Name,
			FrequencyKHz: r.FrequencyKHz,
			Enabled:      r.Enabled,
			IsIntercom:   r.IsIntercom,
		})
	}
	return out
}

func configRadiosToProfile(in []config.Radio) []profile.Radio {
	out := make([]profile.Radio, 0, len(in))
	for _, r := range in {
		out = append(out, profile.Radio{
			ID:           r.ID,
			Name:         r.Name,
			FrequencyKHz: r.FrequencyKHz,
			Enabled:      r.Enabled,
			IsIntercom:   r.IsIntercom,
		})
	}
	return out
}

func profileLayoutToConfig(l profile.Layout) config.CommsLayout {
	blocks := make([]config.LayoutBlock, 0, len(l.Blocks))
	for _, b := range l.Blocks {
		blocks = append(blocks, config.LayoutBlock{RadioID: b.RadioID, W: b.W, H: b.H})
	}
	return config.CommsLayout{WindowW: l.Window.W, WindowH: l.Window.H, Blocks: blocks}
}

func configLayoutToProfile(l config.CommsLayout) profile.Layout {
	blocks := make([]profile.Block, 0, len(l.Blocks))
	for _, b := range l.Blocks {
		blocks = append(blocks, profile.Block{RadioID: b.RadioID, W: b.W, H: b.H})
	}
	return profile.Layout{
		Window: profile.WindowSize{W: l.WindowW, H: l.WindowH},
		Blocks: blocks,
	}
}

// profilesDir resolves where profiles live for this session.
func (a *App) profilesDir() (string, error) {
	sb := a.settings
	if sb == nil {
		return "", fmt.Errorf("profiles: no settings backend")
	}
	sb.mu.Lock()
	configured := sb.cfg.ProfilesDir
	sb.mu.Unlock()
	return config.ProfilesDirPath(configured)
}

// captureProfile renders the CURRENT live state as a profile document.
//
// It deliberately does not capture SelectedRadioID: see selectionFor and
// the phase spec's 4.3.
func (a *App) captureProfile(name, desc string) *profile.Document {
	sb := a.settings
	d := &profile.Document{
		SchemaVersion: profile.SchemaVersion,
		Name:          name,
		Description:   desc,
	}
	if sb == nil {
		return d
	}
	sb.mu.Lock()
	d.Radios = configRadiosToProfile(sb.cfg.Radios)
	d.Layout = configLayoutToProfile(sb.cfg.CommsLayout)
	sb.mu.Unlock()
	d.Reconcile()
	return d
}

// applyProfile writes a loaded document into live config, resizes the Comms
// window, pushes the radios to the server and records the active path.
//
// ONE config.Save covers radios, selection and layout, so there is no
// half-applied state to recover from. The server push comes LAST,
// deliberately: persistRadios already documents the rule that a failed
// local persist must not be masked by a successful server round trip this
// client itself cannot then act on.
//
// It mutates d (d.Reconcile normalises it in place), so callers should not
// rely on d being unchanged afterwards.
//
// A nil settings backend is a no-op returning nil, the discipline every
// other settings-writing method in this package follows so tests that build
// an App with no backend keep working.
func (a *App) applyProfile(d *profile.Document, path string) error {
	sb := a.settings
	if sb == nil {
		return nil
	}
	d.Reconcile()
	radios := profileRadiosToConfig(d.Radios)

	// writeMu is scoped to the snapshot -> mutate -> persist -> restore
	// sequence ONLY, via this closure. The store update, window resize and
	// server push below run with no lock held: the push is an unbounded RPC,
	// and holding writeMu across it would stall every settings write (and
	// RefreshKeybinds, which takes writeMu) behind a server that never answers.
	selected, window, saveErr := func() (uint32, config.CommsLayout, error) {
		sb.writeMu.Lock()
		defer sb.writeMu.Unlock()

		sb.mu.Lock()
		defer sb.mu.Unlock()
		next := *sb.cfg
		next.Radios = radios
		next.SelectedRadioID = selectionFor(radios, next.SelectedRadioID)
		next.CommsLayout = profileLayoutToConfig(d.Layout)
		next.ActiveProfile = path
		var err error
		if sb.cfgPath != "" {
			err = config.Save(sb.cfgPath, &next)
		}
		if err == nil {
			sb.cfg = &next
		}
		return next.SelectedRadioID, next.CommsLayout, err
	}()

	if saveErr != nil {
		return fmt.Errorf("apply profile: %w", saveErr)
	}

	// Keep the in-memory store in step with what was just persisted, the
	// same pairing SetSettingsBackend does at startup.
	if a.st != nil {
		a.st.SetSelectedRadio(selected)
	}
	if a.windows != nil && window.WindowW > 0 && window.WindowH > 0 {
		g := a.windows.Geometry("comms")
		g.W, g.H = window.WindowW, window.WindowH
		a.windows.SetGeometry("comms", g)
	}
	// pushPersistedRadios nil-guards a.sess, so an offline load persists
	// locally and pushes nothing.
	a.pushPersistedRadios(context.Background())
	return nil
}

// profileDirty reports whether live state differs from the active profile's
// file.
//
// COMPUTED, never stored. There is nothing to keep in sync and nothing that
// can go stale across a crash, an external edit, or a config change made by
// another code path.
//
// With NO active profile there is no dirty state: an ad-hoc setup is not
// "unsaved", it simply is not a profile. A profile whose file has VANISHED
// does read as dirty -- the live state is then genuinely unsaved.
func (a *App) profileDirty() bool {
	sb := a.settings
	if sb == nil {
		return false
	}
	sb.mu.Lock()
	path := sb.cfg.ActiveProfile
	live := configRadiosToProfile(sb.cfg.Radios)
	liveLayout := configLayoutToProfile(sb.cfg.CommsLayout)
	sb.mu.Unlock()

	if strings.TrimSpace(path) == "" {
		return false
	}
	saved, err := profile.Read(path)
	if err != nil {
		return true
	}
	cur := &profile.Document{Radios: live, Layout: liveLayout}
	cur.Reconcile()
	return !radiosEqual(cur.Radios, saved.Radios) || !layoutEqual(cur.Layout, saved.Layout)
}

func radiosEqual(a, b []profile.Radio) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func layoutEqual(a, b profile.Layout) bool {
	if a.Window != b.Window || len(a.Blocks) != len(b.Blocks) {
		return false
	}
	for i := range a.Blocks {
		if a.Blocks[i] != b.Blocks[i] {
			return false
		}
	}
	return true
}
