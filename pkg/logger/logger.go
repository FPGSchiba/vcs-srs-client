// Package logger wires log/slog with optional file rotation via lumberjack.
package logger

import (
	"io"
	"log/slog"
	"os"

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

// CreateLogger constructs the default app logger (INFO, JSON to stderr + file).
// Kept for the existing main.go call site; will be replaced by New(...) directly
// in a later task when config supplies the level.
func CreateLogger() *slog.Logger {
	return New(Options{Level: slog.LevelInfo, JSON: true})
}
