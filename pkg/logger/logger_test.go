package logger_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/pkg/logger"
)

func TestNewLogger_WritesJSONToWriter(t *testing.T) {
	var buf bytes.Buffer
	l := logger.New(logger.Options{Level: slog.LevelInfo, Writer: &buf, JSON: true})
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
	l := logger.New(logger.Options{Level: slog.LevelWarn, Writer: &buf, JSON: true})
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
