package profile

import (
	"os"
	"path/filepath"
	"testing"
)

func seedOnce(t *testing.T, dir string, rec map[string]string) map[string]string {
	t.Helper()
	res, err := Seed(dir, rec)
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	out := map[string]string{}
	for k, v := range rec {
		out[k] = v
	}
	for _, r := range res {
		if r.Hash != "" {
			out[r.ID] = r.Hash
		}
	}
	return out
}

func TestBuiltinsAreValidProfiles(t *testing.T) {
	bs := Builtins()
	if len(bs) == 0 {
		t.Fatal("no builtins embedded")
	}
	for _, b := range bs {
		d, err := Decode(b.Content)
		if err != nil {
			t.Fatalf("builtin %s does not parse: %v", b.ID, err)
		}
		if err := d.Validate(); err != nil {
			t.Fatalf("builtin %s does not validate: %v", b.ID, err)
		}
	}
}

// Row 1: no hash recorded + file missing -> WRITE (first run).
func TestSeedWritesOnFirstRun(t *testing.T) {
	dir := t.TempDir()
	rec := seedOnce(t, dir, map[string]string{})
	p := filepath.Join(dir, "standard-fleet"+Ext)
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("builtin not written on first run: %v", err)
	}
	if rec["standard-fleet"] == "" {
		t.Fatal("Seed must return a hash to record for a file it wrote")
	}
}

// Row 2: no hash recorded + file present -> leave (a user file owns the name).
func TestSeedLeavesUnrecordedExistingFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "standard-fleet"+Ext)
	mine := []byte(`{"schema_version":1,"name":"Mine","radios":[]}`)
	if err := os.WriteFile(p, mine, 0o644); err != nil {
		t.Fatal(err)
	}
	seedOnce(t, dir, map[string]string{})
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(mine) {
		t.Fatalf("a pre-existing file with no recorded hash must never be clobbered.\ngot:  %s\nwant: %s", got, mine)
	}
}

// Row 3: hash recorded + file missing -> leave (the user deleted it).
func TestSeedRespectsDeletion(t *testing.T) {
	dir := t.TempDir()
	rec := seedOnce(t, dir, map[string]string{})
	p := filepath.Join(dir, "standard-fleet"+Ext)
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	seedOnce(t, dir, rec)
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("a builtin the user deleted must stay deleted: a recorded hash proves it was written once, so its absence is a decision, not a fresh install")
	}
}

// Row 4: hash recorded + hash matches -> REPLACE when the shipped content differs.
func TestSeedUpdatesUntouchedFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "standard-fleet"+Ext)
	// Pretend an OLDER build shipped different content: record the hash of
	// what is actually on disk, then seed with today's embedded content.
	old := []byte(`{"schema_version":1,"name":"Standard Fleet","radios":[]}`)
	if err := os.WriteFile(p, old, 0o644); err != nil {
		t.Fatal(err)
	}
	rec := map[string]string{"standard-fleet": HashContent(old)}
	seedOnce(t, dir, rec)

	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) == string(old) {
		t.Fatal("an untouched builtin must be replaced when a newer build ships different content")
	}
}

func TestSeedDoesNotRewriteIdenticalContent(t *testing.T) {
	dir := t.TempDir()
	rec := seedOnce(t, dir, map[string]string{})
	res, err := Seed(dir, rec)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res {
		if r.Written {
			t.Fatalf("second seed rewrote %s although nothing changed", r.ID)
		}
	}
}

// Row 5: hash recorded + hash differs -> leave (the user edited it). This is
// the row that protects user data, so it asserts byte identity.
func TestSeedNeverOverwritesUserEdit(t *testing.T) {
	dir := t.TempDir()
	rec := seedOnce(t, dir, map[string]string{})
	p := filepath.Join(dir, "standard-fleet"+Ext)

	edited := []byte(`{"schema_version":1,"name":"Standard Fleet","description":"my tuning","radios":[]}`)
	if err := os.WriteFile(p, edited, 0o644); err != nil {
		t.Fatal(err)
	}
	// Seed twice: a regression that only bites on the second pass is still
	// a regression that destroys the edit.
	rec = seedOnce(t, dir, rec)
	seedOnce(t, dir, rec)

	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(edited) {
		t.Fatalf("a user-edited builtin must never be touched again by any future build.\ngot:  %s\nwant: %s", got, edited)
	}
}

func TestSeedMissingDirIsCreated(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "profiles")
	seedOnce(t, dir, map[string]string{})
	if _, err := os.Stat(filepath.Join(dir, "standard-fleet"+Ext)); err != nil {
		t.Fatalf("Seed must create the profiles dir: %v", err)
	}
}
