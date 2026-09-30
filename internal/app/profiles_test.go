package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/config"
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
