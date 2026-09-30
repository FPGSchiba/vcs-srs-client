package profile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Ext is the profile filename suffix.
const Ext = ".vcs.json"

// Summary is one row of the Profiles directory listing: enough to render
// the table and the layout preview without re-reading every file.
type Summary struct {
	Path        string     `json:"path"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Author      string     `json:"author"`
	ModifiedAt  time.Time  `json:"modified_at"`
	RadioCount  int        `json:"radio_count"`
	Window      WindowSize `json:"window"`
	Blocks      []Block    `json:"blocks"`
}

// List returns every readable profile in dir, sorted by display name.
//
// A MISSING directory lists empty with no error -- the discipline
// windowstate.Load uses for a first run, where nothing has been written
// yet. A path that EXISTS but is not a directory (profiles_dir pointing at
// a regular file) DOES error: that is a misconfiguration the user has to be
// told about, not an empty shelf. (os.ReadDir alone cannot reliably
// distinguish the two across platforms -- on Windows a regular file reports
// os.ErrNotExist, which would silently return an empty listing for a
// misconfigured profiles_dir; Stat pre-checks the path to catch this.)
//
// One unparseable file does not fail the listing. A corrupt profile must
// not hide every other profile the user has; it simply does not appear.
func List(dir string) ([]Summary, error) {
	info, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []Summary{}, nil
		}
		return nil, fmt.Errorf("list profiles in %s: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("list profiles in %s: not a directory", dir)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("list profiles in %s: %w", dir, err)
	}

	out := make([]Summary, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), Ext) {
			continue
		}
		p := filepath.Join(dir, e.Name())
		d, err := Read(p)
		if err != nil {
			continue
		}
		out = append(out, Summary{
			Path:        p,
			Name:        d.Name,
			Description: d.Description,
			Author:      d.Author,
			ModifiedAt:  d.ModifiedAt,
			RadioCount:  len(d.Radios),
			Window:      d.Layout.Window,
			Blocks:      d.Layout.Blocks,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// Read loads, validates and reconciles one profile.
func Read(path string) (*Document, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read profile %s: %w", filepath.Base(path), err)
	}
	d, err := Decode(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	if err := d.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	d.Reconcile()
	return d, nil
}

// Write persists a profile atomically: write-temp then rename, the pattern
// windowstate.Save established. A half-written profile is never visible,
// and a crash mid-write leaves the previous version intact.
//
// It Validates first, then Reconciles, so what lands on disk is always
// self-consistent regardless of what the caller assembled. (Reconciling
// before Validating would repair a document that should have been refused.)
// Note that Write mutates the caller's *Document through Reconcile.
func Write(path string, d *Document) error {
	if err := d.Validate(); err != nil {
		return err
	}
	d.Reconcile()
	b, err := Encode(d)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create profiles dir %s: %w", dir, err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("write profile %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename profile %s: %w", filepath.Base(path), err)
	}
	return nil
}

// Delete removes a profile file. A file that is already gone counts as
// success: the caller's intent -- "this profile should not exist" -- is
// satisfied either way.
func Delete(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete profile %s: %w", filepath.Base(path), err)
	}
	return nil
}

// Slug turns a display name into a filename stem: lowercase ASCII
// alphanumerics and dashes, collapsed. Non-ASCII runes (the em dash in
// "Fleet Op — Stanton") become separators rather than being transliterated
// -- the display name lives INSIDE the file, so the stem only has to be
// stable and safe on all three platforms.
func Slug(name string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	if out := strings.Trim(b.String(), "-"); out != "" {
		return out
	}
	return "profile"
}
