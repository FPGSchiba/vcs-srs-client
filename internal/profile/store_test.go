package profile

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func writeFixture(t *testing.T, dir, base string, d *Document) string {
	t.Helper()
	d.SchemaVersion = SchemaVersion
	b, err := Encode(d)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	p := filepath.Join(dir, base)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return p
}

func TestListSkipsNonProfiles(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "a"+Ext, &Document{
		Name:   "Alpha",
		Radios: []Radio{{ID: 1, Name: "r", FrequencyKHz: 118500}},
	})
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"+Ext), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].Name != "Alpha" || got[0].RadioCount != 1 {
		t.Fatalf("List = %+v, want one Alpha with 1 radio", got)
	}
}

func TestListSkipsUnparseableFileWithoutFailing(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "good"+Ext, &Document{Name: "Good"})
	if err := os.WriteFile(filepath.Join(dir, "bad"+Ext), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := List(dir)
	if err != nil {
		t.Fatalf("one corrupt file must not fail the whole listing: %v", err)
	}
	if len(got) != 1 || got[0].Name != "Good" {
		t.Fatalf("List = %+v, want just Good", got)
	}
}

func TestListMissingDirIsEmptyNotError(t *testing.T) {
	got, err := List(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("a missing profiles dir must list empty, not error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("List = %+v, want empty", got)
	}
}

// Review Focus #3, read half.
func TestListPathIsAFileReportsError(t *testing.T) {
	f := filepath.Join(t.TempDir(), "iam.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := List(f); err == nil {
		t.Fatal("profiles_dir pointing at a regular file must report an error, not silently list nothing")
	}
}

func TestWriteIsAtomicAndCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "profiles")
	p := filepath.Join(dir, "x"+Ext)
	d := &Document{
		SchemaVersion: SchemaVersion,
		Name:          "X",
		Radios:        []Radio{{ID: 1, Name: "r", FrequencyKHz: 118500}},
	}
	if err := Write(p, d); err != nil {
		t.Fatalf("Write: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("dir has %d entries, want exactly 1 (no .tmp left behind): %+v", len(entries), entries)
	}
	back, err := Read(p)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(back.Layout.Blocks) != 1 || back.Layout.Blocks[0].RadioID != 1 {
		t.Fatalf("blocks = %+v, want one reconciled block", back.Layout.Blocks)
	}
}

// Review Focus #3, write half.
func TestWriteToUnwritableDirReportsError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 0500 does not deny writes on Windows")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if err := Write(filepath.Join(dir, "x"+Ext), &Document{SchemaVersion: SchemaVersion, Name: "X"}); err == nil {
		t.Fatal("a write into an unwritable dir must report the failure, not appear to succeed")
	}
}

func TestReadRejectsNewerSchema(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x"+Ext)
	if err := os.WriteFile(p, []byte(`{"schema_version":99,"name":"X"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(p); err == nil {
		t.Fatal("want ErrSchemaTooNew from Read")
	}
}

func TestDeleteRemovesFileAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	p := writeFixture(t, dir, "x"+Ext, &Document{Name: "X"})
	if err := Delete(p); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("file still present after Delete")
	}
	if err := Delete(p); err != nil {
		t.Fatalf("deleting an already-absent profile must succeed: %v", err)
	}
}

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{
		"Fleet Op — Stanton":  "fleet-op-stanton",
		"  Salvage/Crew (R) ": "salvage-crew-r",
		"":                    "profile",
		"////":                "profile",
	} {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSummaryCarriesModifiedTime(t *testing.T) {
	dir := t.TempDir()
	when := time.Date(2026, 5, 19, 21, 0, 0, 0, time.UTC)
	writeFixture(t, dir, "x"+Ext, &Document{Name: "X", ModifiedAt: when})
	got, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !got[0].ModifiedAt.Equal(when) {
		t.Fatalf("ModifiedAt = %v, want %v", got[0].ModifiedAt, when)
	}
}
