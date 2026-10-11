package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFileGivesDefaults(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "none.json"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != Defaults().Port || c.PollIntervalSeconds != 4 {
		t.Fatalf("unexpected defaults: %+v", c)
	}
	if c.LoopbackAddr() != "127.0.0.1:18787" {
		t.Fatalf("addr = %s", c.LoopbackAddr())
	}
}

func TestLoadOverridesAndValidates(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	if err := os.WriteFile(p, []byte(`{"port": 19000, "poll_interval_seconds": 3}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != 19000 || c.PollIntervalSeconds != 3 || len(c.SlackBundleIDs) != 1 {
		t.Fatalf("got %+v", c)
	}

	if err := os.WriteFile(p, []byte(`{"poll_interval_seconds": 999}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal("want validation error")
	}
}

func TestExampleConfigIsValid(t *testing.T) {
	if _, err := Load(filepath.Join("..", "..", "configs", "config.example.json")); err != nil {
		t.Fatal(err)
	}
}

func TestCheckPerms(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	ok := filepath.Join(dir, "ok")
	loose := filepath.Join(dir, "loose")
	if err := os.WriteFile(ok, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(loose, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(loose, 0o644); err != nil {
		t.Fatal(err)
	}
	w := CheckPerms(dir)
	if len(w) != 1 {
		t.Fatalf("warnings = %v", w)
	}
}
