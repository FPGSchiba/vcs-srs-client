package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/config"
	"github.com/FPGSchiba/vcs-srs-client/internal/notify"
	"github.com/FPGSchiba/vcs-srs-client/internal/profile"
	"github.com/FPGSchiba/vcs-srs-client/internal/windowstate"
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
	if a.profilesDirOverride != "" {
		return a.profilesDirOverride, nil
	}
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

// setProfilesDirForTest overrides the resolved profiles directory. Test
// seam only: production resolves through config.ProfilesDirPath, which
// touches the real AppDataDir. Mirrors setCaptureTimeout's role.
func (a *App) setProfilesDirForTest(dir string) { a.profilesDirOverride = dir }

// ListProfiles returns the profile directory listing.
//
// Always non-nil: the frontend types it as an array, and a nil slice
// marshals to null. A directory that cannot be listed notifies once and
// returns empty, so the screen renders an empty table rather than a blank
// page.
func (a *App) ListProfiles() []ProfileSummaryDTO {
	out := []ProfileSummaryDTO{}
	dir, err := a.profilesDir()
	if err != nil {
		a.notifyProfileError("Profiles directory unavailable", err)
		return out
	}
	sums, err := profile.List(dir)
	if err != nil {
		a.notifyProfileError("Cannot read the profiles directory", err)
		return out
	}
	for _, s := range sums {
		blocks := make([]ProfileBlockDTO, 0, len(s.Blocks))
		for _, b := range s.Blocks {
			blocks = append(blocks, ProfileBlockDTO{RadioID: b.RadioID, W: b.W, H: b.H})
		}
		modified := ""
		if !s.ModifiedAt.IsZero() {
			modified = s.ModifiedAt.Local().Format("2006-01-02 15:04")
		}
		out = append(out, ProfileSummaryDTO{
			Path: s.Path, Name: s.Name, Description: s.Description, Author: s.Author,
			Modified: modified, RadioCount: s.RadioCount,
			Window: ProfileWindowDTO{W: s.Window.W, H: s.Window.H},
			Blocks: blocks,
		})
	}
	return out
}

// GetProfileState returns the active profile, its dirty state and the
// resolved directory.
func (a *App) GetProfileState() ProfileStateDTO {
	sb := a.settings
	if sb == nil {
		return ProfileStateDTO{}
	}
	sb.mu.Lock()
	path := sb.cfg.ActiveProfile
	sb.mu.Unlock()

	st := ProfileStateDTO{ActivePath: path, Dirty: a.profileDirty()}
	if dir, err := a.profilesDir(); err == nil {
		st.Dir = dir
	}
	if path != "" {
		if d, err := profile.Read(path); err == nil {
			st.ActiveName = d.Name
		}
	}
	return st
}

// LoadProfile applies a profile from disk.
func (a *App) LoadProfile(path string) error {
	d, err := profile.Read(path)
	if err != nil {
		a.notifyProfileError("Could not load profile", err)
		return err
	}
	if err := a.applyProfile(d, path); err != nil {
		a.notifyProfileError("Could not apply profile", err)
		return err
	}
	a.emitProfileState()
	return nil
}

// SaveProfile overwrites the active profile with the live state, keeping
// its name, description, author and created_at.
func (a *App) SaveProfile() error {
	sb := a.settings
	if sb == nil {
		return nil
	}
	sb.mu.Lock()
	path := sb.cfg.ActiveProfile
	sb.mu.Unlock()
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("no active profile to save; use Save Current As New")
	}
	prev, err := profile.Read(path)
	if err != nil {
		a.notifyProfileError("Could not read the active profile", err)
		return err
	}
	d := a.captureProfile(prev.Name, prev.Description)
	d.Author = prev.Author
	d.CreatedAt = prev.CreatedAt
	d.ModifiedAt = time.Now().UTC()
	if err := profile.Write(path, d); err != nil {
		a.notifyProfileError("Could not save profile", err)
		return err
	}
	a.emitProfileState()
	return nil
}

// SaveProfileAs writes the live state to a NEW profile and activates it.
//
// The filename stem comes from profile.Slug, disambiguated with a numeric
// suffix when it collides: two differently-named profiles that slug
// identically ("Fleet Op" and "Fleet  Op") must not silently overwrite each
// other.
func (a *App) SaveProfileAs(name, desc string) error {
	dir, err := a.profilesDir()
	if err != nil {
		a.notifyProfileError("Profiles directory unavailable", err)
		return err
	}
	path := uniqueProfilePath(dir, profile.Slug(name))
	now := time.Now().UTC()
	d := a.captureProfile(name, desc)
	d.CreatedAt, d.ModifiedAt = now, now
	if err := profile.Write(path, d); err != nil {
		a.notifyProfileError("Could not save profile", err)
		return err
	}
	if err := a.applyProfile(d, path); err != nil {
		a.notifyProfileError("Could not activate the saved profile", err)
		return err
	}
	a.emitProfileState()
	return nil
}

// uniqueProfilePath returns dir/stem.vcs.json, or dir/stem-2.vcs.json and
// so on when that name is taken.
func uniqueProfilePath(dir, stem string) string {
	p := filepath.Join(dir, stem+profile.Ext)
	if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
		return p
	}
	for i := 2; i < 1000; i++ {
		c := filepath.Join(dir, fmt.Sprintf("%s-%d%s", stem, i, profile.Ext))
		if _, err := os.Stat(c); errors.Is(err, os.ErrNotExist) {
			return c
		}
	}
	return p
}

// RenameProfile edits a profile's display name and description in place.
//
// The FILENAME is deliberately left alone: renaming a display name must not
// break a path someone else has a copy of, or that active_profile points
// at.
func (a *App) RenameProfile(path, name, desc string) error {
	d, err := profile.Read(path)
	if err != nil {
		a.notifyProfileError("Could not read profile", err)
		return err
	}
	d.Name, d.Description = name, desc
	d.ModifiedAt = time.Now().UTC()
	if err := profile.Write(path, d); err != nil {
		a.notifyProfileError("Could not rename profile", err)
		return err
	}
	a.emitProfileState()
	return nil
}

// DeleteProfile removes a profile file.
//
// If it was the active one, active_profile is cleared and LIVE CONFIG IS
// LEFT UNTOUCHED -- deleting a saved copy must not wipe the radios you are
// currently using.
func (a *App) DeleteProfile(path string) error {
	// Refuse anything that is not a profile file: the path comes from the
	// frontend and profile.Delete is an os.Remove.
	if !strings.HasSuffix(path, profile.Ext) {
		err := fmt.Errorf("refusing to delete %q: not a %s profile file", path, profile.Ext)
		a.notifyProfileError("Could not delete profile", err)
		return err
	}
	if err := profile.Delete(path); err != nil {
		a.notifyProfileError("Could not delete profile", err)
		return err
	}
	// The save error is captured under the locks and raised AFTER they are
	// released: notify's error-severity OnSound path reaches audioManager,
	// which takes sb.mu, and sync.Mutex is not reentrant.
	var clearErr error
	if sb := a.settings; sb != nil {
		func() {
			sb.writeMu.Lock()
			defer sb.writeMu.Unlock()
			sb.mu.Lock()
			defer sb.mu.Unlock()
			if sb.cfg.ActiveProfile != path {
				return
			}
			next := *sb.cfg
			next.ActiveProfile = ""
			if sb.cfgPath != "" {
				if clearErr = config.Save(sb.cfgPath, &next); clearErr != nil {
					return
				}
			}
			sb.cfg = &next
		}()
	}
	if clearErr != nil {
		a.notifyProfileError("Could not clear the active profile", clearErr)
	}
	a.emitProfileState()
	return nil
}

// RevertProfile re-reads the active profile and re-applies it, discarding
// unsaved changes.
func (a *App) RevertProfile() error {
	sb := a.settings
	if sb == nil {
		return nil
	}
	sb.mu.Lock()
	path := sb.cfg.ActiveProfile
	sb.mu.Unlock()
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("no active profile to revert to")
	}
	return a.LoadProfile(path)
}

// ResetLayout restores the built-in default layout.
//
// LAYOUT ONLY. It does not touch radios or the selected radio: a control
// that silently wiped your tuned frequencies while claiming to reset a
// layout would be a trap. It is the only escape when no profile is loaded.
func (a *App) ResetLayout() error {
	sb := a.settings
	if sb == nil {
		return nil
	}
	// writeMu covers snapshot -> mutate -> persist only; the geometry change
	// and the emit (two file reads plus a dispatch) run after release, as in
	// applyProfile.
	saveErr := func() error {
		sb.writeMu.Lock()
		defer sb.writeMu.Unlock()
		sb.mu.Lock()
		defer sb.mu.Unlock()
		next := *sb.cfg
		blocks := make([]config.LayoutBlock, 0, len(next.Radios))
		for _, r := range next.Radios {
			blocks = append(blocks, config.LayoutBlock{
				RadioID: r.ID, W: profile.DefaultBlockW, H: profile.DefaultBlockH,
			})
		}
		next.CommsLayout = config.CommsLayout{
			WindowW: profile.DefaultWindowW,
			WindowH: profile.DefaultWindowH,
			Blocks:  blocks,
		}
		if sb.cfgPath != "" {
			if err := config.Save(sb.cfgPath, &next); err != nil {
				return err
			}
		}
		sb.cfg = &next
		return nil
	}()
	if saveErr != nil {
		return fmt.Errorf("reset layout: %w", saveErr)
	}
	if a.windows != nil {
		g := a.windows.Geometry("comms")
		a.windows.SetGeometry("comms", windowstate.Geometry{
			X: g.X, Y: g.Y, W: profile.DefaultWindowW, H: profile.DefaultWindowH,
		})
	}
	a.emitProfileState()
	return nil
}

// ImportProfile copies a profile from anywhere on disk into profiles_dir.
//
// A CANCELLED dialog is a normal outcome, not an error: it returns nil and
// notifies nothing.
func (a *App) ImportProfile() error {
	if a.wailsApp == nil {
		return nil
	}
	src, err := a.wailsApp.Dialog.OpenFile().
		SetTitle("Import radio profile").
		AddFilter("VCS radio profile", "*"+profile.Ext).
		PromptForSingleSelection()
	if err != nil || strings.TrimSpace(src) == "" {
		return nil // cancelled
	}
	d, err := profile.Read(src)
	if err != nil {
		a.notifyProfileError("Could not import profile", err)
		return err
	}
	dir, err := a.profilesDir()
	if err != nil {
		a.notifyProfileError("Profiles directory unavailable", err)
		return err
	}
	if err := profile.Write(uniqueProfilePath(dir, profile.Slug(d.Name)), d); err != nil {
		a.notifyProfileError("Could not import profile", err)
		return err
	}
	a.emitProfileState()
	return nil
}

// ExportProfile writes a copy of one profile to a user-chosen path. It does
// not change the active profile.
func (a *App) ExportProfile(path string) error {
	if a.wailsApp == nil {
		return nil
	}
	d, err := profile.Read(path)
	if err != nil {
		a.notifyProfileError("Could not read profile", err)
		return err
	}
	dst, err := a.wailsApp.Dialog.SaveFile().
		SetMessage("Export radio profile").
		SetFilename(profile.Slug(d.Name) + profile.Ext).
		PromptForSingleSelection()
	if err != nil || strings.TrimSpace(dst) == "" {
		return nil // cancelled
	}
	if err := profile.Write(dst, d); err != nil {
		a.notifyProfileError("Could not export profile", err)
		return err
	}
	return nil
}

// BrowseProfilesDir asks for a new profiles directory and persists it.
func (a *App) BrowseProfilesDir() error {
	if a.wailsApp == nil {
		return nil
	}
	dir, err := a.wailsApp.Dialog.OpenFile().
		SetTitle("Choose a profiles folder").
		CanChooseDirectories(true).
		CanChooseFiles(false).
		PromptForSingleSelection()
	if err != nil || strings.TrimSpace(dir) == "" {
		return nil // cancelled
	}
	sb := a.settings
	if sb == nil {
		return nil
	}
	sb.writeMu.Lock()
	sb.mu.Lock()
	next := *sb.cfg
	next.ProfilesDir = dir
	var saveErr error
	if sb.cfgPath != "" {
		saveErr = config.Save(sb.cfgPath, &next)
	}
	if saveErr == nil {
		sb.cfg = &next
	}
	sb.mu.Unlock()
	sb.writeMu.Unlock()
	if saveErr != nil {
		a.notifyProfileError("Could not save the profiles folder", saveErr)
		return saveErr
	}
	a.emitProfileState()
	return nil
}

// OpenProfilesDir reveals the profiles folder in the OS file manager.
func (a *App) OpenProfilesDir() error {
	dir, err := a.profilesDir()
	if err != nil {
		a.notifyProfileError("Profiles directory unavailable", err)
		return err
	}
	return openInFileManager(dir)
}

// SeedBuiltinProfiles writes the shipped default profiles into
// profiles_dir and records their hashes. Called once at startup.
//
// Best-effort: a failure notifies and does not block startup, because one
// unwritable default must not cost the user the others or the session.
func (a *App) SeedBuiltinProfiles() {
	dir, err := a.profilesDir()
	if err != nil {
		a.notifyProfileError("Could not seed default profiles", err)
		return
	}
	sb := a.settings
	if sb == nil {
		return
	}
	sb.mu.Lock()
	recorded := make(map[string]string, len(sb.cfg.BuiltinProfiles))
	for k, v := range sb.cfg.BuiltinProfiles {
		recorded[k] = v
	}
	sb.mu.Unlock()

	results, seedErr := profile.Seed(dir, recorded)
	if seedErr != nil {
		a.notifyProfileError("Could not seed a default profile", seedErr)
	}

	updated := make(map[string]string, len(results))
	for k, v := range recorded {
		updated[k] = v
	}
	changed := false
	for _, r := range results {
		if r.Hash == "" {
			continue
		}
		if updated[r.ID] != r.Hash {
			updated[r.ID] = r.Hash
			changed = true
		}
	}
	if !changed {
		return
	}

	// Locks released before any notify: see DeleteProfile.
	saveErr := func() error {
		sb.writeMu.Lock()
		defer sb.writeMu.Unlock()
		sb.mu.Lock()
		defer sb.mu.Unlock()
		next := *sb.cfg
		next.BuiltinProfiles = updated
		if sb.cfgPath != "" {
			if err := config.Save(sb.cfgPath, &next); err != nil {
				return err
			}
		}
		sb.cfg = &next
		return nil
	}()
	if saveErr != nil {
		// Files are on disk but unrecorded: next startup will treat them as
		// user-owned and never update them.
		a.notifyProfileError("Could not record default profiles", saveErr)
	}
}

// emitProfileState broadcasts the active-profile snapshot to every window.
// The Comms popout renders the dirty dot and the REVERT/RESET controls, and
// it is a separate webview with its own JS heap, so this cannot live in the
// main window's Zustand store.
func (a *App) emitProfileState() {
	sb := a.settings
	if sb == nil || sb.em == nil {
		return
	}
	sb.em.ProfileState(a.GetProfileState())
}

// notifyProfileError raises one error notification for a profile failure.
// Keyed on the title so a repeated failure of the same kind collapses
// through notify's fingerprint rather than stacking.
func (a *App) notifyProfileError(title string, err error) {
	if a.notif == nil || err == nil {
		return
	}
	a.notif.Raise("profile:"+title, notify.Item{
		Category: "Profiles",
		Severity: notify.SeverityError,
		Icon:     "layout",
		Title:    title,
		Body:     err.Error(),
	})
}

// GetCommsLayout returns the live Comms arrangement for a window hydrating
// on mount. Blocks is always non-nil: the frontend types it as an array.
func (a *App) GetCommsLayout() LayoutDTO {
	out := LayoutDTO{Blocks: []ProfileBlockDTO{}}
	sb := a.settings
	if sb == nil {
		return out
	}
	sb.mu.Lock()
	l := sb.cfg.CommsLayout
	sb.mu.Unlock()
	out.Window = ProfileWindowDTO{W: l.WindowW, H: l.WindowH}
	for _, b := range l.Blocks {
		out.Blocks = append(out.Blocks, ProfileBlockDTO{RadioID: b.RadioID, W: b.W, H: b.H})
	}
	return out
}

// SetCommsLayout records a new arrangement IN MEMORY ONLY.
//
// It deliberately does NOT call config.Save. This runs from a drag, and a
// debounced save on a pointer-move path would put disk I/O behind every
// block the user nudges -- to protect state that is re-created with one
// more drag. The bytes land at shutdown, via flushConfig from
// ServiceShutdown.
//
// In practice the window is much narrower than "everything since launch":
// config.Save serialises the WHOLE Config, so persistRadios,
// persistSelectedRadio and the settings writers all carry the pending
// layout to disk as a side effect of ordinary use.
//
// Dirty state is unaffected by any of this: App.profileDirty compares the
// IN-MEMORY config, so the dot is correct the instant a block moves.
func (a *App) SetCommsLayout(l LayoutDTO) error {
	sb := a.settings
	if sb == nil {
		return nil
	}
	blocks := make([]config.LayoutBlock, 0, len(l.Blocks))
	for _, b := range l.Blocks {
		w, h := b.W, b.H
		if w < profile.MinBlockW {
			w = profile.MinBlockW
		}
		if h < profile.MinBlockH {
			h = profile.MinBlockH
		}
		blocks = append(blocks, config.LayoutBlock{RadioID: b.RadioID, W: w, H: h})
	}
	sb.mu.Lock()
	next := *sb.cfg
	next.CommsLayout = config.CommsLayout{WindowW: l.Window.W, WindowH: l.Window.H, Blocks: blocks}
	sb.cfg = &next
	sb.layoutPending = true
	// Unlock explicitly, NOT via defer: emitProfileState re-enters sb.mu
	// through GetProfileState and profileDirty, and a deferred unlock would
	// move it inside the critical section. This is a self-deadlock trap;
	// see the SetCommsLayoutDoesNotDeadlockOnEmit test and Task 9's fix.
	sb.mu.Unlock()
	a.emitProfileState()
	return nil
}

// flushConfig persists the in-memory config. Called from ServiceShutdown so
// a layout that only ever lived in memory reaches disk on quit.
//
// Errors are returned for the test's benefit; ServiceShutdown logs and
// carries on, because there is no window left to notify into and a failed
// layout write must not block the quit.
//
// Only flushes if layoutPending is set, preventing a user with a corrupt
// config file from having it silently replaced with defaults on quit when
// no drag has occurred.
func (a *App) flushConfig() error {
	sb := a.settings
	if sb == nil || sb.cfgPath == "" {
		return nil
	}
	sb.mu.Lock()
	if !sb.layoutPending {
		sb.mu.Unlock()
		return nil
	}
	sb.mu.Unlock()
	sb.writeMu.Lock()
	defer sb.writeMu.Unlock()
	sb.mu.Lock()
	snapshot := *sb.cfg
	sb.layoutPending = false
	sb.mu.Unlock()
	if err := config.Save(sb.cfgPath, &snapshot); err != nil {
		return fmt.Errorf("flush config: %w", err)
	}
	return nil
}
