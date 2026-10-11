package procctl

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"slack-mention-relay/internal/config"
)

const (
	// ReadyFD is the descriptor number on which `run` reports startup status.
	ReadyFD = 3
	// ReadyLine is what `run` writes on ReadyFD once its preflight passes.
	ReadyLine = "ready"

	caffeinateBin = "/usr/bin/caffeinate"
	stopGrace     = 3 * time.Second
)

// Manager turns the relay on and off.
type Manager struct {
	Paths config.Paths
	Cfg   config.Config
	// Self is the path of this binary, used to spawn `run`.
	Self string
	// ReadyTimeout bounds how long On waits for `run` to report ready.
	ReadyTimeout time.Duration
}

// Status lists the pids of the live processes (0 = not running).
type Status struct {
	Run, Bark, Caffeinate int
}

// On reports whether the watcher is running; it defines the relay state.
func (s Status) On() bool { return s.Run != 0 }

func (m *Manager) selfName() string { return filepath.Base(m.Self) }
func (m *Manager) barkName() string { return filepath.Base(m.Paths.BarkServer) }

// Status inspects pid files, removing stale ones.
func (m *Manager) Status() Status {
	var s Status
	s.Run, _ = Running(PIDFile(m.Paths.RunPID), m.selfName())
	s.Bark, _ = Running(PIDFile(m.Paths.BarkPID), m.barkName())
	s.Caffeinate, _ = Running(PIDFile(m.Paths.CaffeinePID), filepath.Base(caffeinateBin))
	return s
}

// On starts bark-server, waits for it to listen on loopback, starts `run`
// detached, waits for its preflight, then starts caffeinate bound to it.
// On any failure everything started so far is stopped again.
func (m *Manager) On(ctx context.Context) (err error) {
	if err := config.EnsureDir(m.Paths); err != nil {
		return fmt.Errorf("データディレクトリを作れません: %w", err)
	}
	// Clean leftovers from a crashed session before starting fresh.
	if err := m.Off(); err != nil {
		return err
	}
	if fi, statErr := os.Stat(m.Paths.BarkServer); statErr != nil || fi.Mode().Perm()&0o100 == 0 {
		return errors.New("bark-server が見つかりません（scripts/install-bark-server.sh を実行してください）")
	}

	var started []*Child
	var pidFiles []PIDFile
	defer func() {
		if err != nil {
			for i := len(started) - 1; i >= 0; i-- {
				StopChild(started[i], stopGrace)
			}
			for _, pf := range pidFiles {
				pf.Remove()
			}
		}
	}()
	record := func(c *Child, pf PIDFile) error {
		started = append(started, c)
		pidFiles = append(pidFiles, pf)
		return pf.Write(c.Pid())
	}

	// 1. bark-server on loopback only.
	addr := m.Cfg.LoopbackAddr()
	spec, err := BarkServerSpec(m.Paths.BarkServer, addr, m.Paths.BarkDataDir, false, nil)
	if err != nil {
		return err
	}
	bark, err := Spawn(spec)
	if err != nil {
		return fmt.Errorf("bark-server を起動できません: %w", err)
	}
	if err := record(bark, PIDFile(m.Paths.BarkPID)); err != nil {
		return err
	}

	// 2. Wait until it accepts connections on 127.0.0.1.
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := WaitListening(wctx, addr, func() bool { return !bark.Exited() }); err != nil {
		return fmt.Errorf("bark-server: %v", err)
	}

	// 3. The watcher, detached, reporting readiness on fd 3.
	run, err := m.startRun()
	if err != nil {
		return err
	}
	if err := record(run, PIDFile(m.Paths.RunPID)); err != nil {
		return err
	}

	// 4. caffeinate -i (system sleep only) for as long as the watcher lives.
	caf, err := Spawn(Spec{Path: caffeinateBin, Args: []string{"-i", "-w", strconv.Itoa(run.Pid())}})
	if err != nil {
		return fmt.Errorf("caffeinate を起動できません: %w", err)
	}
	return record(caf, PIDFile(m.Paths.CaffeinePID))
}

func (m *Manager) startRun() (*Child, error) {
	logf, err := os.OpenFile(m.Paths.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("ログファイルを開けません: %w", err)
	}
	defer logf.Close()

	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	run, err := Spawn(Spec{
		Path:       m.Self,
		Args:       []string{"run", "--ready-fd", strconv.Itoa(ReadyFD)},
		Stderr:     logf,
		ExtraFiles: []*os.File{w}, // becomes fd 3 in the child
	})
	_ = w.Close() // the child holds its own copy
	if err != nil {
		return nil, fmt.Errorf("watcher を起動できません: %w", err)
	}

	timeout := m.ReadyTimeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	_ = r.SetReadDeadline(time.Now().Add(timeout))
	line, _ := bufio.NewReader(r).ReadString('\n')
	line = strings.TrimSpace(line)
	if line == ReadyLine {
		return run, nil
	}
	StopChild(run, stopGrace)
	if msg, ok := strings.CutPrefix(line, "error: "); ok {
		return nil, errors.New(msg)
	}
	return nil, errors.New("watcher が起動しませんでした（~/.slack-relay/relay.log を確認）")
}

// Off stops the watcher, caffeinate and bark-server. It is idempotent.
func (m *Manager) Off() error {
	var errs []error
	stop := func(pf PIDFile, name string) {
		if pid, ok := Running(pf, name); ok {
			if err := Terminate(pid, stopGrace); err != nil {
				errs = append(errs, err)
				return
			}
		}
		pf.Remove()
	}
	stop(PIDFile(m.Paths.RunPID), m.selfName())
	stop(PIDFile(m.Paths.CaffeinePID), filepath.Base(caffeinateBin))
	stop(PIDFile(m.Paths.BarkPID), m.barkName())
	return errors.Join(errs...)
}
