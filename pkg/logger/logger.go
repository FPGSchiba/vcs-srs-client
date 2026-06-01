// Package logger wires log/slog with optional file rotation via lumberjack.
package logger

import (
	"io"
	"log/slog"
	"os"
	"strings"

	"gopkg.in/natefinch/lumberjack.v2"
)

// Options configures the logger. Writer overrides FilePath when set (used in tests).
type Options struct {
	Level    slog.Level
	Writer   io.Writer // if nil, writes to stderr + rotating file
	JSON     bool      // true = JSON encoder; false = text encoder
	FilePath string    // ignored if Writer is set; default: "log/vcs-client.log"
}

// New constructs a *slog.Logger using the given options.
func New(opt Options) *slog.Logger {
	w := opt.Writer
	if w == nil {
		path := opt.FilePath
		if path == "" {
			path = "log/vcs-client.log"
		}
		file := &lumberjack.Logger{Filename: path, MaxSize: 10, MaxBackups: 3, MaxAge: 180}
		w = io.MultiWriter(os.Stderr, file)
	}
	handlerOpts := &slog.HandlerOptions{Level: opt.Level, AddSource: true}
	var handler slog.Handler
	if opt.JSON {
		handler = slog.NewJSONHandler(w, handlerOpts)
	} else {
		handler = slog.NewTextHandler(w, handlerOpts)
	}
	return slog.New(handler)
}

// ParseLevel maps a config log-level string to a slog.Level. Matching is
// case-insensitive; "WARNING" is accepted as an alias for "WARN". Empty or
// unrecognised values fall back to INFO.
func ParseLevel(s string) slog.Level {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "DEBUG":
		return slog.LevelDebug
	case "WARN", "WARNING":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
