package profile

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// builtinFS holds the profiles shipped with the app. Same mechanism as
// internal/audio/sfx.go's asset embed.
//
//go:embed builtin
var builtinFS embed.FS

// Builtin is one shipped default profile. ID is the filename stem and is
// the key under which its hash is recorded in config.
type Builtin struct {
	ID      string
	Content []byte
}

// SeedResult reports what Seed decided for one builtin.
//
// Hash is what the caller must RECORD for this id: the new content's hash
// when Written, the caller's existing record when not, and "" when there is
// nothing to record (rows 2 and 3 -- a file that is not ours, and a file
// the user deleted, whose record must be preserved as-is by the caller
// rather than overwritten or cleared).
type SeedResult struct {
	ID      string
	Path    string
	Hash    string
	Written bool
}

// HashContent is the content fingerprint the seeding decision turns on.
func HashContent(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Builtins returns every embedded default profile, ordered by id.
func Builtins() []Builtin {
	entries, err := builtinFS.ReadDir("builtin")
	if err != nil {
		return nil
	}
	out := make([]Builtin, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), Ext) {
			continue
		}
		b, err := builtinFS.ReadFile("builtin/" + e.Name())
		if err != nil {
			continue
		}
		out = append(out, Builtin{
			ID:      strings.TrimSuffix(e.Name(), Ext),
			Content: b,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Seed writes the shipped default profiles into dir, using recorded (id ->
// hash of the content THIS CLIENT last wrote) to decide what may be
// touched:
//
//	recorded | on disk            | action
//	---------+--------------------+----------------------------------------
//	none     | missing            | WRITE   -- first run
//	none     | present            | leave   -- a user file owns that name
//	present  | missing            | leave   -- the user deleted it
//	present  | hash == record     | REPLACE if the shipped content differs
//	present  | hash != record     | leave   -- the user edited it
//
// Two properties are the whole point of the scheme, and both are load
// bearing enough to have their own tests:
//
//   - A user edit is PERMANENT. Once the on-disk hash diverges from the
//     record, that file is never written again by any future build. There
//     is no version in which a shipped update silently reverts someone's
//     tuning.
//   - A deletion is PERMANENT. A recorded hash proves the file was written
//     at least once, so its absence is a decision, not a fresh install.
//
// Seeding is best-effort per builtin: one failure is returned but does not
// stop the others, because a single unwritable file must not cost the user
// every other default.
func Seed(dir string, recorded map[string]string) ([]SeedResult, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create profiles dir %s: %w", dir, err)
	}

	var firstErr error
	bs := Builtins()
	out := make([]SeedResult, 0, len(bs))
	for _, b := range bs {
		res := SeedResult{ID: b.ID, Path: filepath.Join(dir, b.ID+Ext)}
		rec, haveRec := recorded[b.ID]

		onDisk, readErr := os.ReadFile(res.Path)
		missing := errors.Is(readErr, os.ErrNotExist)
		if readErr != nil && !missing {
			if firstErr == nil {
				firstErr = fmt.Errorf("read builtin %s: %w", b.ID, readErr)
			}
			out = append(out, res)
			continue
		}

		switch {
		case !haveRec && missing:
			// First run.
		case !haveRec:
			// A file we have never written owns this name. Not ours.
			out = append(out, res)
			continue
		case missing:
			// Deleted deliberately. Keep the record so it stays deleted.
			res.Hash = rec
			out = append(out, res)
			continue
		case HashContent(onDisk) != rec:
			// Edited by the user. Keep the record so we keep recognising it
			// as theirs rather than treating it as a fresh name next start.
			res.Hash = rec
			out = append(out, res)
			continue
		case HashContent(onDisk) == HashContent(b.Content):
			// Untouched and already current.
			res.Hash = rec
			out = append(out, res)
			continue
		}

		if err := os.WriteFile(res.Path, b.Content, 0o644); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("write builtin %s: %w", b.ID, err)
			}
			out = append(out, res)
			continue
		}
		res.Written = true
		res.Hash = HashContent(b.Content)
		out = append(out, res)
	}
	return out, firstErr
}
