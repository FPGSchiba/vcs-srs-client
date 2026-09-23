// Package logger wires log/slog with optional file rotation via lumberjack.
package logger

import (
	"io"
	"log/slog"
	"os"
	"strings"

	"gopkg.in/natefinch/lumberjack.v2"
)

// BestEffort wraps w so that its write errors can never abort an
// io.MultiWriter chain: Write always reports a full, successful write.
//
// io.MultiWriter.Write returns on the FIRST writer's error and never reaches
// the rest. A production Windows build links with -H windowsgui (see
// build/windows/Taskfile.yml), so a binary launched from Explorer has no
// console and every os.Stderr.Write fails with ERROR_INVALID_HANDLE. Without
// this wrapper the rotating log file -- the only sink that exists on such a
// build, and the one the joystick hardware-verification checklist reads
// "joystick bind fired" from -- would receive nothing at all, and the
// verifier would read an empty file as "the binding did not fire".
//
// Only stderr is wrapped. The file writer stays unwrapped so a genuinely
// broken log file still surfaces its error to the caller.
func BestEffort(w io.Writer) io.Writer { return bestEffort{w: w} }

type bestEffort struct{ w io.Writer }

// Write attempts the underlying write and then reports success regardless.
func (b bestEffort) Write(p []byte) (int, error) {
	b.w.Write(p) //nolint:errcheck // deliberately ignored; see BestEffort's doc
	return len(p), nil
}

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
		// stderr first and best-effort, the file second and unwrapped: a dead
		// console can then never starve the file, while a broken file still
		// reports its own error. See BestEffort.
		w = io.MultiWriter(BestEffort(os.Stderr), file)
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
