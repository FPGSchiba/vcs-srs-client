package app

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestFileManagerBinary(t *testing.T) {
	noLook := func(string) (string, error) { t.Fatal("lookPath must not be used"); return "", nil }

	got, err := fileManagerBinary("windows", `D:\Win`, noLook)
	if err != nil || got != filepath.Join(`D:\Win`, "explorer.exe") {
		t.Fatalf("windows = %q, %v", got, err)
	}
	got, _ = fileManagerBinary("windows", "", noLook)
	if got != filepath.Join(`C:\Windows`, "explorer.exe") {
		t.Fatalf("windows fallback = %q", got)
	}
	if got, _ = fileManagerBinary("darwin", "", noLook); got != "/usr/bin/open" {
		t.Fatalf("darwin = %q", got)
	}

	got, err = fileManagerBinary("linux", "", func(n string) (string, error) {
		if n != "xdg-open" {
			t.Fatalf("looked up %q", n)
		}
		return "/usr/bin/xdg-open", nil
	})
	if err != nil || got != "/usr/bin/xdg-open" {
		t.Fatalf("linux = %q, %v", got, err)
	}
	want := errors.New("not found")
	if _, err = fileManagerBinary("linux", "", func(string) (string, error) { return "", want }); !errors.Is(err, want) {
		t.Fatalf("linux missing err = %v", err)
	}
}
