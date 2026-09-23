package logger_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/pkg/logger"
)

func TestNewLogger_WritesJSONToWriter(t *testing.T) {
	var buf bytes.Buffer
	l, _ := logger.New(logger.Options{Level: slog.LevelInfo, Writer: &buf, JSON: true})
	l.Info("hello", "k", "v")

	if buf.Len() == 0 {
		t.Fatal("expected logger to write output, got empty buffer")
	}
	var got map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &got); err != nil {
		t.Fatalf("expected valid JSON, got: %s (err: %v)", buf.String(), err)
	}
	if got["msg"] != "hello" || got["k"] != "v" {
		t.Fatalf("expected msg=hello k=v, got: %v", got)
	}
}

func TestNewLogger_FiltersByLevel(t *testing.T) {
	var buf bytes.Buffer
	l, _ := logger.New(logger.Options{Level: slog.LevelWarn, Writer: &buf, JSON: true})
	l.Info("filtered out")
	l.Warn("kept")

	if !strings.Contains(buf.String(), "kept") {
		t.Fatalf("expected 'kept' in output, got: %s", buf.String())
	}
	if strings.Contains(buf.String(), "filtered out") {
		t.Fatalf("expected info to be filtered, but it was in output")
	}
}

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"DEBUG":   slog.LevelDebug,
		"debug":   slog.LevelDebug,
		"INFO":    slog.LevelInfo,
		"warn":    slog.LevelWarn,
		"WARNING": slog.LevelWarn,
		"ERROR":   slog.LevelError,
		"":        slog.LevelInfo, // empty → default INFO
		"bogus":   slog.LevelInfo, // unknown → default INFO
	}
	for in, want := range cases {
		if got := logger.ParseLevel(in); got != want {
			t.Errorf("ParseLevel(%q) = %v, want %v", in, got, want)
		}
	}
}

// errWriter fails every Write, standing in for os.Stderr on a production
// Windows build: -H windowsgui means the process has no console, so every
// os.Stderr.Write returns ERROR_INVALID_HANDLE.
type errWriter struct{ n int }

func (e *errWriter) Write(p []byte) (int, error) {
	e.n++
	return 0, errors.New("no console")
}

// TestMultiWriterStarvesLaterSinksOnAnEarlyError documents WHY logger.New
// wraps stderr: io.MultiWriter.Write returns on the FIRST writer's error and
// never reaches the rest. This half must keep failing to deliver, otherwise
// the wrapped half below would pass for the wrong reason.
func TestMultiWriterStarvesLaterSinksOnAnEarlyError(t *testing.T) {
	var file bytes.Buffer
	dead := &errWriter{}
	w := io.MultiWriter(dead, &file)

	n, err := w.Write([]byte("hello\n"))
	if err == nil {
		t.Fatal("expected the dead first writer to abort the MultiWriter")
	}
	if n != 0 {
		t.Errorf("n = %d, want 0", n)
	}
	if file.Len() != 0 {
		t.Fatalf("expected the file sink to be STARVED by the dead stderr, got %q", file.String())
	}
}

// TestBestEffortKeepsTheFileSinkAlive is the contrast: with the dead writer
// wrapped, the SAME MultiWriter must still deliver every byte to the file.
// On a production Windows build the rotating log file is the only sink that
// exists, and it is what docs/superpowers/plans/2026-09-18-joystick-manual-
// verification.md reads to confirm a joystick binding fired.
func TestBestEffortKeepsTheFileSinkAlive(t *testing.T) {
	var file bytes.Buffer
	dead := &errWriter{}
	w := io.MultiWriter(logger.BestEffort(dead), &file)

	n, err := w.Write([]byte("hello\n"))
	if err != nil {
		t.Fatalf("Write returned %v, want nil: a dead stderr must not surface as an error", err)
	}
	if n != len("hello\n") {
		t.Errorf("n = %d, want %d", n, len("hello\n"))
	}
	if got := file.String(); got != "hello\n" {
		t.Fatalf("file sink got %q, want %q", got, "hello\n")
	}
	if dead.n != 1 {
		t.Errorf("wrapped writer was called %d times, want 1 (best-effort still ATTEMPTS the write)", dead.n)
	}
}

// TestBestEffortStillSurfacesTheFileWritersError guards the other direction:
// only stderr is wrapped, so a failing FILE sink must remain reportable.
func TestBestEffortStillSurfacesTheFileWritersError(t *testing.T) {
	w := io.MultiWriter(logger.BestEffort(&errWriter{}), &errWriter{})
	if _, err := w.Write([]byte("hello\n")); err == nil {
		t.Fatal("expected the unwrapped file writer's error to surface")
	}
}

// TestNewOrdersTheFileSinkAfterStderr pins the wiring: New's default writer
// must tolerate a dead stderr end to end, not merely have a helper available.
//
// It also exercises New's Closer: lumberjack keeps the log file's OS handle
// open across writes (that's what makes rotation possible), and nothing
// else in the package ever closes it. On Windows an open handle blocks
// t.TempDir()'s own cleanup from removing dir -- "The process cannot
// access the file because it is being used by another process" -- so this
// test must close it itself before that cleanup runs. defer does that: it
// fires when this function returns, which is always before t.Cleanup
// funcs (TempDir's RemoveAll included) run.
func TestNewOrdersTheFileSinkAfterStderr(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vcs-client.log")
	l, closer := logger.New(logger.Options{Level: slog.LevelInfo, JSON: true, FilePath: path})
	defer closer.Close() //nolint:errcheck // best-effort cleanup; the assertions below are what matters
	l.Info("joystick bind fired")

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("rotating log file was never written: %v", err)
	}
	if !strings.Contains(string(b), "joystick bind fired") {
		t.Fatalf("log file = %q, want the logged line", string(b))
	}
}
