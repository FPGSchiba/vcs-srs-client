package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/config"
	"github.com/FPGSchiba/vcs-srs-client/internal/events"
	"github.com/FPGSchiba/vcs-srs-client/internal/notify"
	"github.com/FPGSchiba/vcs-srs-client/internal/profile"
)

func TestSelectionForKeepsAValidSelection(t *testing.T) {
	radios := []config.Radio{
		{ID: 1, Name: "a", FrequencyKHz: 118500, Enabled: true},
		{ID: 2, Name: "b", FrequencyKHz: 122750, Enabled: true},
	}
	if got := selectionFor(radios, 2); got != 2 {
		t.Fatalf("selectionFor = %d, want the existing selection 2", got)
	}
}

func TestSelectionForFallsBackWhenRadioIsGone(t *testing.T) {
	// The silently-dead-PTT case: LOAD swaps the radio set wholesale, and
	// resolveTXTarget returns nil for a missing id -- a green PTT light
	// over a transmission nobody hears.
	radios := []config.Radio{
		{ID: 7, Name: "a", FrequencyKHz: 118500, Enabled: true},
		{ID: 8, Name: "b", FrequencyKHz: 122750, Enabled: true},
	}
	if got := selectionFor(radios, 3); got != 7 {
		t.Fatalf("selectionFor = %d, want the first ENABLED radio 7", got)
	}
}

func TestSelectionForSkipsDisabledRadios(t *testing.T) {
	radios := []config.Radio{
		{ID: 1, Name: "a", FrequencyKHz: 118500, Enabled: false},
		{ID: 2, Name: "b", FrequencyKHz: 122750, Enabled: true},
	}
	if got := selectionFor(radios, 99); got != 2 {
		t.Fatalf("selectionFor = %d, want 2 -- a disabled radio cannot be a PTT target", got)
	}
}

func TestSelectionForDisabledCurrentIsReplaced(t *testing.T) {
	radios := []config.Radio{
		{ID: 1, Name: "a", FrequencyKHz: 118500, Enabled: false},
		{ID: 2, Name: "b", FrequencyKHz: 122750, Enabled: true},
	}
	if got := selectionFor(radios, 1); got != 2 {
		t.Fatalf("selectionFor = %d, want 2 -- the stored selection is present but disabled", got)
	}
}

// Review Focus #4.
func TestSelectionForEmptyProfileIsZero(t *testing.T) {
	if got := selectionFor(nil, 5); got != 0 {
		t.Fatalf("selectionFor = %d, want 0 -- an empty profile leaves nothing selected, and the Comms window shows its 'No radios' empty state", got)
	}
}

func TestApplyProfileWritesConfigOnce(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	a := newTestAppWithConfig(t, config.Default(), cfgPath)
	d := &profile.Document{
		SchemaVersion: profile.SchemaVersion,
		Name:          "Fleet Op",
		Radios: []profile.Radio{
			{ID: 1, Name: "Fleet", FrequencyKHz: 118500, Enabled: true},
		},
		Layout: profile.Layout{
			Window: profile.WindowSize{W: 600, H: 800},
			Blocks: []profile.Block{{RadioID: 1, W: 400, H: 150}},
		},
	}
	path := filepath.Join(t.TempDir(), "fleet-op"+profile.Ext)
	if err := a.applyProfile(d, path); err != nil {
		t.Fatalf("applyProfile: %v", err)
	}

	back, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Radios) != 1 || back.Radios[0].FrequencyKHz != 118500 {
		t.Fatalf("radios = %+v, want the profile's, as INTEGER kHz", back.Radios)
	}
	if back.SelectedRadioID != 1 {
		t.Fatalf("SelectedRadioID = %d, want 1 (re-validated against the new set)", back.SelectedRadioID)
	}
	if back.CommsLayout.WindowW != 600 || len(back.CommsLayout.Blocks) != 1 {
		t.Fatalf("CommsLayout = %+v", back.CommsLayout)
	}
	if back.ActiveProfile != path {
		t.Fatalf("ActiveProfile = %q, want %q", back.ActiveProfile, path)
	}
}

func TestApplyProfileDoesNotRoundTripFrequencyThroughFloat(t *testing.T) {
	// The one silent failure in this phase. 118_501 kHz has no exact
	// float32 representation as MHz, so a format or mapping that went via
	// a float would come back changed.
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	a := newTestAppWithConfig(t, config.Default(), cfgPath)
	d := &profile.Document{
		SchemaVersion: profile.SchemaVersion,
		Name:          "x",
		Radios:        []profile.Radio{{ID: 1, Name: "r", FrequencyKHz: 118501, Enabled: true}},
	}
	if err := a.applyProfile(d, filepath.Join(t.TempDir(), "x"+profile.Ext)); err != nil {
		t.Fatal(err)
	}
	back, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if back.Radios[0].FrequencyKHz != 118501 {
		t.Fatalf("FrequencyKHz = %d, want 118501 exactly", back.Radios[0].FrequencyKHz)
	}
}

func TestCaptureProfileRoundTripsThroughApply(t *testing.T) {
	a := newTestAppWithConfig(t, config.Default(), filepath.Join(t.TempDir(), "config.toml"))
	dir := t.TempDir()
	src := &profile.Document{
		SchemaVersion: profile.SchemaVersion,
		Name:          "Src",
		Radios: []profile.Radio{
			{ID: 1, Name: "Fleet", FrequencyKHz: 118500, Enabled: true},
			{ID: 2, Name: "Wing", FrequencyKHz: 122750, Enabled: true, IsIntercom: true},
		},
		Layout: profile.Layout{
			Window: profile.WindowSize{W: 610, H: 810},
			Blocks: []profile.Block{{RadioID: 2, W: 300, H: 120}, {RadioID: 1, W: 400, H: 150}},
		},
	}
	if err := a.applyProfile(src, filepath.Join(dir, "src"+profile.Ext)); err != nil {
		t.Fatal(err)
	}
	got := a.captureProfile("Src", "")
	if len(got.Radios) != 2 || got.Radios[1].FrequencyKHz != 122750 || !got.Radios[1].IsIntercom {
		t.Fatalf("radios = %+v", got.Radios)
	}
	if got.Layout.Blocks[0].RadioID != 2 {
		t.Fatalf("block ORDER must survive the round trip: %+v", got.Layout.Blocks)
	}
	if got.Layout.Window.W != 610 {
		t.Fatalf("window = %+v", got.Layout.Window)
	}
}

func TestProfileDirtyIsFalseRightAfterLoad(t *testing.T) {
	a := newTestAppWithConfig(t, config.Default(), filepath.Join(t.TempDir(), "config.toml"))
	dir := t.TempDir()
	path := filepath.Join(dir, "x"+profile.Ext)
	d := &profile.Document{
		SchemaVersion: profile.SchemaVersion,
		Name:          "X",
		Radios:        []profile.Radio{{ID: 1, Name: "r", FrequencyKHz: 118500, Enabled: true}},
	}
	if err := profile.Write(path, d); err != nil {
		t.Fatal(err)
	}
	loaded, err := profile.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.applyProfile(loaded, path); err != nil {
		t.Fatal(err)
	}
	if a.profileDirty() {
		t.Fatal("a freshly loaded profile must not be dirty")
	}
}

func TestProfileDirtyAfterLayoutChangeButNotAfterSelectionChange(t *testing.T) {
	a := newTestAppWithConfig(t, config.Default(), filepath.Join(t.TempDir(), "config.toml"))
	dir := t.TempDir()
	path := filepath.Join(dir, "x"+profile.Ext)
	d := &profile.Document{
		SchemaVersion: profile.SchemaVersion,
		Name:          "X",
		Radios: []profile.Radio{
			{ID: 1, Name: "r", FrequencyKHz: 118500, Enabled: true},
			{ID: 2, Name: "s", FrequencyKHz: 122750, Enabled: true},
		},
	}
	if err := profile.Write(path, d); err != nil {
		t.Fatal(err)
	}
	loaded, _ := profile.Read(path)
	if err := a.applyProfile(loaded, path); err != nil {
		t.Fatal(err)
	}

	// Selecting a different radio must NOT dirty the profile: that is the
	// whole reason selected_radio_id is not stored in the document.
	if err := a.SelectRadio(2); err != nil {
		t.Fatal(err)
	}
	if a.profileDirty() {
		t.Fatal("a radio-select must not dirty the profile -- it is the control hit most often mid-operation")
	}

	// Resizing a block MUST dirty it.
	sb := a.settings
	sb.mu.Lock()
	next := *sb.cfg
	next.CommsLayout.Blocks = []config.LayoutBlock{{RadioID: 1, W: 999, H: 150}, {RadioID: 2, W: 400, H: 150}}
	sb.cfg = &next
	sb.mu.Unlock()
	if !a.profileDirty() {
		t.Fatal("a block resize must dirty the profile")
	}
}

func TestProfileDirtyIsFalseWithNoActiveProfile(t *testing.T) {
	a := newTestAppWithConfig(t, config.Default(), filepath.Join(t.TempDir(), "config.toml"))
	if a.profileDirty() {
		t.Fatal("with no active profile there is nothing to compare against, so nothing is unsaved")
	}
}

func TestProfileDirtyWhenActiveFileVanished(t *testing.T) {
	a := newTestAppWithConfig(t, config.Default(), filepath.Join(t.TempDir(), "config.toml"))
	dir := t.TempDir()
	path := filepath.Join(dir, "x"+profile.Ext)
	d := &profile.Document{
		SchemaVersion: profile.SchemaVersion,
		Name:          "X",
		Radios:        []profile.Radio{{ID: 1, Name: "r", FrequencyKHz: 118500, Enabled: true}},
	}
	if err := profile.Write(path, d); err != nil {
		t.Fatal(err)
	}
	loaded, _ := profile.Read(path)
	if err := a.applyProfile(loaded, path); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if !a.profileDirty() {
		t.Fatal("a profile whose file is gone reads as dirty: the live state is genuinely unsaved")
	}
}

func TestApplyProfileSaveFailureLeavesStateUntouched(t *testing.T) {
	// A cfgPath that is a directory makes config.Save fail.
	cfgPath := t.TempDir()
	cfg := config.Default()
	cfg.Radios = []config.Radio{{ID: 9, Name: "old", FrequencyKHz: 121500, Enabled: true}}
	cfg.CommsLayout = config.CommsLayout{WindowW: 500, WindowH: 700}
	cfg.ActiveProfile = "old-path"
	a := newTestAppWithConfig(t, cfg, cfgPath)
	fc := &fakeControlSession{}
	a.sess = fc

	d := &profile.Document{
		SchemaVersion: profile.SchemaVersion,
		Name:          "New",
		Radios:        []profile.Radio{{ID: 1, Name: "n", FrequencyKHz: 118500, Enabled: true}},
		Layout:        profile.Layout{Window: profile.WindowSize{W: 600, H: 800}},
	}
	if err := a.applyProfile(d, "new-path"); err == nil {
		t.Fatal("applyProfile must return an error when config.Save fails")
	}
	sb := a.settings
	sb.mu.Lock()
	got := *sb.cfg
	sb.mu.Unlock()
	if len(got.Radios) != 1 || got.Radios[0].ID != 9 || got.CommsLayout.WindowW != 500 {
		t.Fatalf("live state changed despite failed save: radios=%+v layout=%+v", got.Radios, got.CommsLayout)
	}
	if got.ActiveProfile != "old-path" {
		t.Fatalf("ActiveProfile = %q, want unchanged", got.ActiveProfile)
	}
	if n := fc.updateCount(); n != 0 {
		t.Fatalf("server push happened %d time(s) after a failed save", n)
	}
}

// newProfileTestApp builds an App over a real on-disk config, so tests can
// read back what a binding persisted.
func newProfileTestApp(t *testing.T) (*App, string) {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	return newTestAppWithConfig(t, config.Default(), cfgPath), cfgPath
}

func TestSaveProfileAsWritesAndActivates(t *testing.T) {
	a, _ := newProfileTestApp(t)
	dir := t.TempDir()
	a.setProfilesDirForTest(dir)

	sb := a.settings
	sb.mu.Lock()
	next := *sb.cfg
	next.Radios = []config.Radio{{ID: 1, Name: "Fleet", FrequencyKHz: 118500, Enabled: true}}
	sb.cfg = &next
	sb.mu.Unlock()

	if err := a.SaveProfileAs("Fleet Op — Stanton", "notes"); err != nil {
		t.Fatalf("SaveProfileAs: %v", err)
	}
	want := filepath.Join(dir, "fleet-op-stanton"+profile.Ext)
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected %s: %v", want, err)
	}
	if st := a.GetProfileState(); st.ActivePath != want || st.ActiveName != "Fleet Op — Stanton" || st.Dirty {
		t.Fatalf("state = %+v", st)
	}
}

func TestSaveProfileAsDisambiguatesCollidingSlug(t *testing.T) {
	a, _ := newProfileTestApp(t)
	dir := t.TempDir()
	a.setProfilesDirForTest(dir)
	if err := a.SaveProfileAs("Fleet Op", ""); err != nil {
		t.Fatal(err)
	}
	if err := a.SaveProfileAs("Fleet  Op", ""); err != nil {
		t.Fatal(err)
	}
	got, err := profile.List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("two profiles whose names slug identically must not overwrite each other, got %d: %+v", len(got), got)
	}
}

func TestDeleteActiveProfileLeavesLiveConfigIntact(t *testing.T) {
	a, cfgPath := newProfileTestApp(t)
	dir := t.TempDir()
	a.setProfilesDirForTest(dir)

	sb := a.settings
	sb.mu.Lock()
	next := *sb.cfg
	next.Radios = []config.Radio{{ID: 1, Name: "Fleet", FrequencyKHz: 118500, Enabled: true}}
	sb.cfg = &next
	sb.mu.Unlock()
	if err := a.SaveProfileAs("X", ""); err != nil {
		t.Fatal(err)
	}
	active := a.GetProfileState().ActivePath

	if err := a.DeleteProfile(active); err != nil {
		t.Fatalf("DeleteProfile: %v", err)
	}
	back, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Radios) != 1 {
		t.Fatal("deleting a saved copy must not wipe the radios you are currently using")
	}
	if back.ActiveProfile != "" {
		t.Fatalf("ActiveProfile = %q, want cleared", back.ActiveProfile)
	}
}

func TestResetLayoutLeavesRadiosAlone(t *testing.T) {
	a, _ := newProfileTestApp(t)
	sb := a.settings
	sb.mu.Lock()
	next := *sb.cfg
	next.Radios = []config.Radio{{ID: 1, Name: "Fleet", FrequencyKHz: 118500, Enabled: true}}
	next.SelectedRadioID = 1
	next.CommsLayout = config.CommsLayout{WindowW: 999, WindowH: 999,
		Blocks: []config.LayoutBlock{{RadioID: 1, W: 999, H: 999}}}
	sb.cfg = &next
	sb.mu.Unlock()

	if err := a.ResetLayout(); err != nil {
		t.Fatalf("ResetLayout: %v", err)
	}
	sb.mu.Lock()
	got := *sb.cfg
	sb.mu.Unlock()
	if len(got.Radios) != 1 || got.SelectedRadioID != 1 {
		t.Fatal("RESET is layout-only: a control that wiped tuned frequencies while claiming to reset a layout is a trap")
	}
	if got.CommsLayout.WindowW != profile.DefaultWindowW {
		t.Fatalf("window = %+v, want the built-in default", got.CommsLayout)
	}
	if got.CommsLayout.Blocks[0].W != profile.DefaultBlockW {
		t.Fatalf("blocks = %+v, want default sizes", got.CommsLayout.Blocks)
	}
}

func TestRevertRestoresTheSavedProfile(t *testing.T) {
	a, _ := newProfileTestApp(t)
	dir := t.TempDir()
	a.setProfilesDirForTest(dir)
	sb := a.settings
	sb.mu.Lock()
	next := *sb.cfg
	next.Radios = []config.Radio{{ID: 1, Name: "Fleet", FrequencyKHz: 118500, Enabled: true}}
	sb.cfg = &next
	sb.mu.Unlock()
	if err := a.SaveProfileAs("X", ""); err != nil {
		t.Fatal(err)
	}

	sb.mu.Lock()
	n2 := *sb.cfg
	n2.CommsLayout.Blocks = []config.LayoutBlock{{RadioID: 1, W: 999, H: 999}}
	sb.cfg = &n2
	sb.mu.Unlock()
	if !a.profileDirty() {
		t.Fatal("precondition: should be dirty")
	}

	if err := a.RevertProfile(); err != nil {
		t.Fatalf("RevertProfile: %v", err)
	}
	if a.profileDirty() {
		t.Fatal("REVERT must leave the profile clean")
	}
}

func TestRevertWithNoActiveProfileIsAnError(t *testing.T) {
	a, _ := newProfileTestApp(t)
	if err := a.RevertProfile(); err == nil {
		t.Fatal("REVERT with nothing active has nothing to revert to")
	}
}

func TestLoadProfileNotifiesOnMalformedFile(t *testing.T) {
	a, _ := newProfileTestApp(t)
	n := notify.New(notify.Options{})
	a.setNotifier(n)
	p := filepath.Join(t.TempDir(), "bad"+profile.Ext)
	if err := os.WriteFile(p, []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.LoadProfile(p); err == nil {
		t.Fatal("want an error")
	}
	items := n.Snapshot().Items
	if len(items) == 0 {
		t.Fatal("a malformed profile must raise a notification naming the file")
	}
	if !strings.Contains(items[0].Body, "bad") {
		t.Fatalf("notification must name the file: %+v", items[0])
	}
}

func TestSeedBuiltinProfilesRecordsHashes(t *testing.T) {
	a, cfgPath := newProfileTestApp(t)
	dir := t.TempDir()
	a.setProfilesDirForTest(dir)

	a.SeedBuiltinProfiles()

	back, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.BuiltinProfiles) == 0 {
		t.Fatal("seeding must record the hash of every builtin it wrote")
	}
	got, err := profile.List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("no builtin written")
	}
}

func TestSeedBuiltinProfilesIsIdempotent(t *testing.T) {
	a, _ := newProfileTestApp(t)
	dir := t.TempDir()
	a.setProfilesDirForTest(dir)
	a.SeedBuiltinProfiles()
	first, err := profile.List(dir)
	if err != nil {
		t.Fatal(err)
	}
	a.SeedBuiltinProfiles()
	second, err := profile.List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != len(second) {
		t.Fatalf("second seed changed the directory: %d -> %d", len(first), len(second))
	}
}

func TestRenameProfileLeavesFilenameAlone(t *testing.T) {
	a, _ := newProfileTestApp(t)
	dir := t.TempDir()
	a.setProfilesDirForTest(dir)
	if err := a.SaveProfileAs("Old Name", "d"); err != nil {
		t.Fatal(err)
	}
	path := a.GetProfileState().ActivePath
	if err := a.RenameProfile(path, "New Name", "new desc"); err != nil {
		t.Fatal(err)
	}
	d, err := profile.Read(path)
	if err != nil {
		t.Fatalf("the file must still be at its old path: %v", err)
	}
	if d.Name != "New Name" || d.Description != "new desc" {
		t.Fatalf("doc = %+v", d)
	}
	if got := a.GetProfileState(); got.ActivePath != path || got.ActiveName != "New Name" {
		t.Fatalf("state = %+v", got)
	}
}

// An empty SeedResult.Hash means "not ours, nothing to record" and must never
// clear an existing record.
func TestSeedBuiltinProfilesKeepsRecordForUserDeletedBuiltin(t *testing.T) {
	a, cfgPath := newProfileTestApp(t)
	dir := t.TempDir()
	a.setProfilesDirForTest(dir)
	a.SeedBuiltinProfiles()
	before, err := config.Load(cfgPath)
	if err != nil || len(before.BuiltinProfiles) == 0 {
		t.Fatalf("precondition: %v %+v", err, before)
	}
	sums, _ := profile.List(dir)
	for _, s := range sums {
		if err := os.Remove(s.Path); err != nil {
			t.Fatal(err)
		}
	}
	a.SeedBuiltinProfiles()
	after, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.BuiltinProfiles) != len(before.BuiltinProfiles) {
		t.Fatalf("records cleared: %+v -> %+v", before.BuiltinProfiles, after.BuiltinProfiles)
	}
	if left, _ := profile.List(dir); len(left) != 0 {
		t.Fatalf("a user-deleted builtin must stay deleted, got %d", len(left))
	}
}

func TestSeedBuiltinProfilesToleratesNilRecordMap(t *testing.T) {
	cfg := config.Default()
	cfg.BuiltinProfiles = nil
	a := newTestAppWithConfig(t, cfg, filepath.Join(t.TempDir(), "config.toml"))
	a.setProfilesDirForTest(t.TempDir())
	a.SeedBuiltinProfiles() // must not panic on a nil map
}

func TestProfileDialogBindingsWithoutWailsAppAreNoOps(t *testing.T) {
	a, _ := newProfileTestApp(t)
	if err := a.ImportProfile(); err != nil {
		t.Fatal(err)
	}
	if err := a.ExportProfile("whatever"); err != nil {
		t.Fatal(err)
	}
	if err := a.BrowseProfilesDir(); err != nil {
		t.Fatal(err)
	}
}

func TestListProfilesIsNeverNil(t *testing.T) {
	a, _ := newProfileTestApp(t)
	a.setProfilesDirForTest(t.TempDir())
	if got := a.ListProfiles(); got == nil {
		t.Fatal("nil marshals to null; the frontend types this as an array")
	}
}

func TestProfileBindingsEmitProfileState(t *testing.T) {
	a, _ := newProfileTestApp(t)
	a.setProfilesDirForTest(t.TempDir())
	rec := &recordingEmitter{}
	a.settings.em = events.New(rec)
	if err := a.SaveProfileAs("X", ""); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range rec.names() {
		if n == events.EventProfileState {
			found = true
		}
	}
	if !found {
		t.Fatalf("no %s emitted: %v", events.EventProfileState, rec.names())
	}
}

// Regression: DeleteProfile used to raise its save-failure notification while
// holding sb.mu. notify's error-severity OnSound reaches audioManager, which
// takes sb.mu -- a permanent self-deadlock. The notifier here has an OnSound
// that takes sb.mu exactly as audioManager does.
func TestDeleteProfileSaveFailureDoesNotDeadlockOnNotify(t *testing.T) {
	a, _ := newProfileTestApp(t)
	dir := t.TempDir()
	a.setProfilesDirForTest(dir)
	sb := a.settings
	a.setNotifier(notify.New(notify.Options{OnSound: func(notify.Item) {
		sb.mu.Lock()
		defer sb.mu.Unlock()
	}}))
	if err := a.SaveProfileAs("X", ""); err != nil {
		t.Fatal(err)
	}
	active := a.GetProfileState().ActivePath

	// Make config.Save fail: its parent "directory" is a regular file.
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	sb.mu.Lock()
	sb.cfgPath = filepath.Join(blocker, "config.toml")
	sb.mu.Unlock()

	done := make(chan error, 1)
	go func() { done <- a.DeleteProfile(active) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("DeleteProfile deadlocked: notification raised while holding sb.mu")
	}
	if a.notif.Snapshot().Items[0].Title != "Could not clear the active profile" {
		t.Fatalf("the save failure must still be reported: %+v", a.notif.Snapshot().Items)
	}
}

func TestDeleteProfileRefusesNonProfileFiles(t *testing.T) {
	a, _ := newProfileTestApp(t)
	victim := filepath.Join(t.TempDir(), "precious.txt")
	if err := os.WriteFile(victim, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteProfile(victim); err == nil {
		t.Fatal("want an error for a path that is not a profile file")
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("file must survive: %v", err)
	}
}

func TestSetCommsLayoutWritesNoFile(t *testing.T) {
	a, cfgPath := newProfileTestApp(t)
	// Save the config to disk first to ensure the file exists.
	sb := a.settings
	sb.mu.Lock()
	snapshot := *sb.cfg
	sb.mu.Unlock()
	if err := config.Save(cfgPath, &snapshot); err != nil {
		t.Fatal(err)
	}

	st, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	before := st.ModTime()

	time.Sleep(10 * time.Millisecond)
	if err := a.SetCommsLayout(LayoutDTO{
		Window: ProfileWindowDTO{W: 700, H: 900},
		Blocks: []ProfileBlockDTO{{RadioID: 1, W: 400, H: 150}},
	}); err != nil {
		t.Fatalf("SetCommsLayout: %v", err)
	}

	st2, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !st2.ModTime().Equal(before) {
		t.Fatal("a drag must not write config.toml: no disk I/O on a pointer-move path")
	}
}

func TestSetCommsLayoutIsVisibleImmediatelyInMemory(t *testing.T) {
	a, _ := newProfileTestApp(t)
	if err := a.SetCommsLayout(LayoutDTO{
		Window: ProfileWindowDTO{W: 700, H: 900},
		Blocks: []ProfileBlockDTO{{RadioID: 1, W: 400, H: 150}},
	}); err != nil {
		t.Fatal(err)
	}
	got := a.GetCommsLayout()
	if got.Window.W != 700 || len(got.Blocks) != 1 || got.Blocks[0].W != 400 {
		t.Fatalf("GetCommsLayout = %+v, want the in-memory value", got)
	}
}

func TestSetCommsLayoutDirtiesImmediately(t *testing.T) {
	// Dirty compares IN-MEMORY config, so it must be correct the instant a
	// block moves -- regardless of when the bytes land on disk.
	a, _ := newProfileTestApp(t)
	dir := t.TempDir()
	a.setProfilesDirForTest(dir)
	sb := a.settings
	sb.mu.Lock()
	next := *sb.cfg
	next.Radios = []config.Radio{{ID: 1, Name: "r", FrequencyKHz: 118500, Enabled: true}}
	sb.cfg = &next
	sb.mu.Unlock()
	if err := a.SaveProfileAs("X", ""); err != nil {
		t.Fatal(err)
	}
	if a.profileDirty() {
		t.Fatal("precondition: clean right after save")
	}

	if err := a.SetCommsLayout(LayoutDTO{
		Window: ProfileWindowDTO{W: 999, H: 999},
		Blocks: []ProfileBlockDTO{{RadioID: 1, W: 999, H: 999}},
	}); err != nil {
		t.Fatal(err)
	}
	if !a.profileDirty() {
		t.Fatal("a resize must dirty the profile immediately, before any file is written")
	}
}

func TestFlushConfigWritesTheLayout(t *testing.T) {
	a, cfgPath := newProfileTestApp(t)
	if err := a.SetCommsLayout(LayoutDTO{
		Window: ProfileWindowDTO{W: 700, H: 900},
		Blocks: []ProfileBlockDTO{{RadioID: 1, W: 400, H: 150}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.flushConfig(); err != nil {
		t.Fatalf("flushConfig: %v", err)
	}
	back, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if back.CommsLayout.WindowW != 700 || len(back.CommsLayout.Blocks) != 1 {
		t.Fatalf("CommsLayout on disk = %+v", back.CommsLayout)
	}
}

func TestLayoutFreeRidesOnAnUnrelatedConfigSave(t *testing.T) {
	// config.Save serialises the WHOLE Config, so any other settings write
	// carries the layout to disk long before quit. This is what makes the
	// crash window much narrower than "everything since launch".
	a, cfgPath := newProfileTestApp(t)
	if err := a.SetCommsLayout(LayoutDTO{
		Window: ProfileWindowDTO{W: 701, H: 901},
		Blocks: []ProfileBlockDTO{{RadioID: 1, W: 401, H: 151}},
	}); err != nil {
		t.Fatal(err)
	}
	// Any existing settings writer will do; persistSelectedRadio is the
	// smallest.
	if err := a.persistSelectedRadio(0); err != nil {
		t.Fatal(err)
	}
	back, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if back.CommsLayout.WindowW != 701 {
		t.Fatalf("an unrelated config.Save must carry the pending layout: %+v", back.CommsLayout)
	}
}

func TestFlushConfigWithoutLayoutChangeWritesNothing(t *testing.T) {
	// If SetCommsLayout is never called, flushConfig must not write, even
	// when called at shutdown. This guards against overwriting a corrupt
	// config file with defaults when the user never dragged a block.
	a, cfgPath := newProfileTestApp(t)
	// Save the config to disk first to establish a baseline.
	sb := a.settings
	sb.mu.Lock()
	snapshot := *sb.cfg
	sb.mu.Unlock()
	if err := config.Save(cfgPath, &snapshot); err != nil {
		t.Fatal(err)
	}

	st, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	before := st.ModTime()

	time.Sleep(10 * time.Millisecond)
	// Do NOT call SetCommsLayout; flushConfig with layoutPending false
	// must be a no-op.
	if err := a.flushConfig(); err != nil {
		t.Fatalf("flushConfig: %v", err)
	}

	st2, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !st2.ModTime().Equal(before) {
		t.Fatal("flushConfig with no layout change must not write to disk")
	}
}

func TestSetCommsLayoutDoesNotDeadlockOnEmit(t *testing.T) {
	// Regression test for the trap where emitProfileState (called outside
	// sb.mu) re-enters sb.mu through GetProfileState and profileDirty.
	// If SetCommsLayout mistakenly used defer sb.mu.Unlock(), the unlock
	// would be deferred to the end of the function, putting the emit inside
	// the critical section and causing a self-deadlock. This goroutine will
	// hang if the deadlock occurs.
	a, _ := newProfileTestApp(t)
	sb := a.settings
	a.setNotifier(notify.New(notify.Options{OnSound: func(notify.Item) {
		// Mimic emitProfileState reaching the audio manager, which takes
		// sb.mu, same as the deadlock regression test for DeleteProfile.
		sb.mu.Lock()
		defer sb.mu.Unlock()
	}}))

	done := make(chan error, 1)
	go func() {
		done <- a.SetCommsLayout(LayoutDTO{
			Window: ProfileWindowDTO{W: 700, H: 900},
			Blocks: []ProfileBlockDTO{{RadioID: 1, W: 400, H: 150}},
		})
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("SetCommsLayout deadlocked: emitProfileState must be called outside sb.mu")
	}
}
