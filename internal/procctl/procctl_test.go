package procctl

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"
)

func TestValidateListenAddr(t *testing.T) {
	cases := []struct {
		addr     string
		allowLAN bool
		ok       bool
	}{
		{"127.0.0.1:18787", false, true},
		{"[::1]:18787", false, true},
		{"0.0.0.0:8080", false, false},
		{"0.0.0.0:8080", true, false}, // wildcard is never allowed, even for register
		{"[::]:8080", true, false},
		{"192.168.1.20:18787", false, false},
		{"192.168.1.20:18787", true, true},
		{"10.1.2.3:18787", true, true},
		{"203.0.113.5:18787", true, false}, // not a private range
		{"localhost:18787", false, false},
		{"127.0.0.1", false, false},
		{"127.0.0.1:0", false, false},
	}
	for _, c := range cases {
		err := ValidateListenAddr(c.addr, c.allowLAN)
		if (err == nil) != c.ok {
			t.Errorf("ValidateListenAddr(%q, %v) = %v, want ok=%v", c.addr, c.allowLAN, err, c.ok)
		}
	}
}

func TestBarkServerSpecRefusesNonLoopback(t *testing.T) {
	if _, err := BarkServerSpec("/x/bark-server", "0.0.0.0:8080", "/d", false, nil); err == nil {
		t.Fatal("wildcard accepted")
	}
	s, err := BarkServerSpec("/x/bark-server", "127.0.0.1:18787", "/d", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--addr", "127.0.0.1:18787", "--data", "/d"}
	for i := range want {
		if s.Args[i] != want[i] {
			t.Fatalf("args = %v", s.Args)
		}
	}
	if s.Stdout != nil {
		t.Fatal("access log must be discarded by default")
	}
}

func TestPIDFileAndRunning(t *testing.T) {
	dir := t.TempDir()
	pf := PIDFile(filepath.Join(dir, "x.pid"))

	c, err := Spawn(Spec{Path: "/bin/sleep", Args: []string{"30"}})
	if err != nil {
		t.Fatal(err)
	}
	defer StopChild(c, time.Second)
	if err := pf.Write(c.Pid()); err != nil {
		t.Fatal(err)
	}
	if pid, ok := Running(pf, "sleep"); !ok || pid != c.Pid() {
		t.Fatalf("Running = %d, %v", pid, ok)
	}
	// Same pid but a different executable name: treated as stale (pid reuse).
	if _, ok := Running(pf, "bark-server"); ok {
		t.Fatal("name mismatch should not count as running")
	}
	if _, err := pf.Read(); err == nil {
		t.Fatal("stale pid file should have been removed")
	}

	StopChild(c, time.Second)
	if !c.Exited() || Alive(c.Pid()) {
		t.Fatal("child still alive after StopChild")
	}
	_ = pf.Write(c.Pid())
	if _, ok := Running(pf, "sleep"); ok {
		t.Fatal("dead pid reported running")
	}
}

func TestSpawnUsesMinimalEnv(t *testing.T) {
	t.Setenv("BARK_SERVER_ADDRESS", "0.0.0.0:8080")
	t.Setenv("HTTPS_PROXY", "http://proxy.invalid:3128")
	var out bytes.Buffer
	c, err := Spawn(Spec{Path: "/usr/bin/env", Stdout: &out})
	if err != nil {
		t.Fatal(err)
	}
	<-c.done
	for _, bad := range []string{"BARK_SERVER_ADDRESS", "HTTPS_PROXY"} {
		if bytes.Contains(out.Bytes(), []byte(bad)) {
			t.Errorf("child inherited %s", bad)
		}
	}
}

func TestLockIsExclusive(t *testing.T) {
	p := filepath.Join(t.TempDir(), "toggle.lock")
	unlock, err := Lock(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Lock(p); err == nil {
		t.Fatal("second lock succeeded")
	}
	unlock()
	unlock2, err := Lock(p)
	if err != nil {
		t.Fatal(err)
	}
	unlock2()
}
