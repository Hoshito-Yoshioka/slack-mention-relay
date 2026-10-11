// Package procctl starts and stops the relay's processes (bark-server, the
// watcher, caffeinate) and tracks them with pid files.
package procctl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ErrNotAllowedAddr is returned for listen addresses that would expose
// bark-server beyond what is permitted.
var ErrNotAllowedAddr = errors.New("procctl: listen address not allowed")

// ValidateListenAddr enforces P11: loopback only, unless allowLAN (the
// `register` command), in which case a private-range LAN IP is also accepted.
// Wildcard addresses (0.0.0.0, ::) and host names are always rejected.
func ValidateListenAddr(addr string, allowLAN bool) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return ErrNotAllowedAddr
	}
	if p, err := strconv.Atoi(port); err != nil || p < 1 || p > 65535 {
		return ErrNotAllowedAddr
	}
	ip := net.ParseIP(host)
	switch {
	case ip == nil, ip.IsUnspecified():
		return ErrNotAllowedAddr
	case ip.IsLoopback():
		return nil
	case allowLAN && ip.IsPrivate():
		return nil
	}
	return ErrNotAllowedAddr
}

// PIDFile is a file holding one decimal pid.
type PIDFile string

func (p PIDFile) Read() (int, error) {
	b, err := os.ReadFile(string(p))
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 1 {
		return 0, fmt.Errorf("procctl: bad pid file %s", filepath.Base(string(p)))
	}
	return pid, nil
}

// Write stores pid atomically with mode 0600.
func (p PIDFile) Write(pid int) error {
	tmp := string(p) + ".tmp"
	if err := os.WriteFile(tmp, []byte(strconv.Itoa(pid)+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, string(p))
}

func (p PIDFile) Remove() { _ = os.Remove(string(p)) }

// Alive reports whether a process with pid exists (kill -0).
func Alive(pid int) bool {
	if pid <= 1 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// commName returns the executable basename of pid via ps(1).
var commName = func(pid int) string {
	out, err := exec.Command("/bin/ps", "-o", "comm=", "-p", strconv.Itoa(pid)).Output() // #nosec G204 -- fixed binary, numeric arg
	if err != nil {
		return ""
	}
	return filepath.Base(strings.TrimSpace(string(out)))
}

// Running returns the pid recorded in pf if that process is alive and its
// executable is named name. A stale pid file is removed.
func Running(pf PIDFile, name string) (int, bool) {
	pid, err := pf.Read()
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			pf.Remove()
		}
		return 0, false
	}
	if Alive(pid) && commName(pid) == name {
		return pid, true
	}
	pf.Remove()
	return 0, false
}

// Spec describes a detached child process.
type Spec struct {
	Path       string
	Args       []string
	Stdout     io.Writer // nil = /dev/null
	Stderr     io.Writer // nil = /dev/null
	ExtraFiles []*os.File
}

// minimalEnv avoids passing through variables such as BARK_SERVER_ADDRESS
// or HTTP(S)_PROXY that could change a child's behaviour.
func minimalEnv() []string {
	env := []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin"}
	for _, k := range []string{"HOME", "USER", "LOGNAME", "TMPDIR"} {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// Child is a process started by this process. It is reaped exactly once
// in the background so that its exit can be observed.
type Child struct {
	Cmd  *exec.Cmd
	done chan struct{}
}

// Pid returns the child's pid.
func (c *Child) Pid() int { return c.Cmd.Process.Pid }

// Exited reports whether the child has terminated.
func (c *Child) Exited() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

// Spawn starts spec in a new session so it outlives the caller.
func Spawn(spec Spec) (*Child, error) {
	cmd := exec.Command(spec.Path, spec.Args...) // #nosec G204 -- paths come from our own config
	cmd.Env = minimalEnv()
	cmd.Dir = "/"
	cmd.Stdin = nil
	cmd.Stdout = spec.Stdout
	cmd.Stderr = spec.Stderr
	cmd.ExtraFiles = spec.ExtraFiles
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := &Child{Cmd: cmd, done: make(chan struct{})}
	go func() { _ = cmd.Wait(); close(c.done) }()
	return c, nil
}

// BarkServerSpec builds the bark-server command line after validating addr.
func BarkServerSpec(bin, addr, dataDir string, allowLAN bool, stdout io.Writer) (Spec, error) {
	if err := ValidateListenAddr(addr, allowLAN); err != nil {
		return Spec{}, err
	}
	// stdout carries the access log (URLs and request bodies): discard it
	// unless the caller explicitly consumes it in memory (register).
	return Spec{
		Path:   bin,
		Args:   []string{"--addr", addr, "--data", dataDir},
		Stdout: stdout,
	}, nil
}

// WaitListening dials addr until it accepts a TCP connection.
func WaitListening(ctx context.Context, addr string, alive func() bool) error {
	var d net.Dialer
	for {
		c, err := d.DialContext(ctx, "tcp", addr)
		if err == nil {
			_ = c.Close()
			return nil
		}
		if alive != nil && !alive() {
			return errors.New("process exited before listening")
		}
		select {
		case <-ctx.Done():
			return errors.New("timed out waiting for listen")
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// Terminate sends SIGTERM to a non-child pid, then SIGKILL after grace.
func Terminate(pid int, grace time.Duration) error {
	if !Alive(pid) {
		return nil
	}
	_ = syscall.Kill(pid, syscall.SIGTERM)
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if !Alive(pid) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	time.Sleep(100 * time.Millisecond)
	if Alive(pid) {
		return fmt.Errorf("procctl: pid %d did not exit", pid)
	}
	return nil
}

// StopChild terminates a process started by this process and reaps it.
func StopChild(c *Child, grace time.Duration) {
	if c == nil {
		return
	}
	_ = c.Cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-c.done:
	case <-time.After(grace):
		_ = c.Cmd.Process.Kill()
		<-c.done
	}
}

// Lock takes an exclusive, non-blocking flock on path. It guards against
// overlapping toggles (e.g. a double-pressed hotkey).
func Lock(path string) (unlock func(), err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304 -- our own lock file
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, errors.New("another toggle is in progress")
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
