package buildcheck

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoPath returns the absolute path of rel, relative to the repository root
// (two levels above this package).
func repoPath(t *testing.T, rel string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "..", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("resolve %s: %v", rel, err)
	}
	return abs
}

// readRepoFile returns the contents of a repository file; missing is fatal.
// CRLF line endings are normalised so text assertions hold on any checkout.
func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(repoPath(t, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

// mustNotExist fails the test if rel exists in the repository.
func mustNotExist(t *testing.T, rel string) {
	t.Helper()
	_, err := os.Stat(repoPath(t, rel))
	if err == nil {
		t.Errorf("%s exists; it must not", rel)
		return
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("stat %s: %v", rel, err)
	}
}
