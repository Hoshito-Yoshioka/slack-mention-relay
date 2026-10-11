package redact

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type kindErr struct{}

func (kindErr) Error() string { return "POST http://127.0.0.1:1/SECRETDEVICEKEY failed" }
func (kindErr) Kind() string  { return "connection_refused" }

func TestSensitiveAttrsAreMasked(t *testing.T) {
	for _, debug := range []bool{false, true} {
		var buf bytes.Buffer
		log := New(&buf, debug)
		log.Debug("dbg", "body", "secret body text")
		log.Info("notification",
			"channel", "#secret-channel",
			"sender", "Secret Sender",
			"body", "secret body text",
			"title", "secret title",
			"device_key", "SECRETDEVICEKEY",
			"key", "0123456789abcdefghijklmnopqrstuv",
			"rec_id", 42,
		)
		log.Error("send failed", "err", fmt.Errorf("wrap: %w", kindErr{}))
		log.Error("send failed", "err", errors.New("raw error with secret body text"))

		out := buf.String()
		for _, s := range []string{"secret-channel", "Secret Sender", "secret body text", "secret title", "SECRETDEVICEKEY", "0123456789abcdef"} {
			if strings.Contains(out, s) {
				t.Errorf("debug=%v: log leaked %q:\n%s", debug, s, out)
			}
		}
		for _, s := range []string{"rec_id=42", "err=connection_refused", "err=error"} {
			if !strings.Contains(out, s) {
				t.Errorf("debug=%v: log missing %q:\n%s", debug, s, out)
			}
		}
	}
}

func TestHomePath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	got := HomePath(filepath.Join(home, ".slack-relay", "relay.log"))
	if got != filepath.Join("~", ".slack-relay", "relay.log") {
		t.Fatalf("got %q", got)
	}
	if HomePath("/tmp/x") != "/tmp/x" {
		t.Fatal("non-home path changed")
	}
}
