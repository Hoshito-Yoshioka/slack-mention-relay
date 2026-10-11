// Package redact provides a logger that never writes notification content
// or secrets. It works on an allow list: only attributes whose keys are known
// to be harmless are written verbatim; every other attribute is masked. The
// same rules apply in debug mode.
package redact

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
)

const Mask = "[REDACTED]"

// Attribute keys that are safe to log as-is. Anything else is masked.
var allowed = map[string]bool{
	"rec_id":   true,
	"count":    true,
	"attempt":  true,
	"status":   true,
	"pid":      true,
	"addr":     true,
	"interval": true,
	"delay":    true,
	"path":     true,
	"state":    true,
	"step":     true,
}

// Kinder is implemented by errors that can describe themselves without
// leaking content (e.g. "connection_refused", "http_status_500").
type Kinder interface {
	Kind() string
}

// ErrKind returns a content-free classification of err.
func ErrKind(err error) string {
	if err == nil {
		return ""
	}
	var k Kinder
	if errors.As(err, &k) {
		return k.Kind()
	}
	return "error"
}

// HomePath replaces the user's home directory with "~" so that log lines
// pasted into issues do not reveal the account name.
func HomePath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if strings.HasPrefix(p, home+string(os.PathSeparator)) {
		return "~" + p[len(home):]
	}
	return p
}

func replace(_ []string, a slog.Attr) slog.Attr {
	switch a.Key {
	case slog.TimeKey, slog.LevelKey, slog.MessageKey:
		return a
	case "err", "error":
		if e, ok := a.Value.Any().(error); ok {
			return slog.String(a.Key, ErrKind(e))
		}
		return slog.String(a.Key, Mask)
	}
	if a.Value.Kind() == slog.KindGroup {
		return a // members are passed through replace individually
	}
	if !allowed[a.Key] {
		return slog.String(a.Key, Mask)
	}
	if a.Value.Kind() == slog.KindString {
		return slog.String(a.Key, HomePath(a.Value.String()))
	}
	return a
}

// New returns a text logger writing to w with redaction applied.
// Messages passed to the logger must be constant strings; dynamic values go
// into attributes so that they are subject to the allow list.
func New(w io.Writer, debug bool) *slog.Logger {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: replace,
	}))
}
