// Command slack-mention-relay relays Slack desktop mentions from the macOS
// Notification Center to an iPhone via a local bark-server, end-to-end
// encrypted.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"slack-mention-relay/internal/config"
	"slack-mention-relay/internal/procctl"
	"slack-mention-relay/internal/secrets"
)

const usage = `usage: slack-mention-relay <command>

commands:
  toggle       relay を ON/OFF する（Raycast から呼ばれる）
  status       現在の状態を表示する
  setup        AES 鍵を生成し、device_key を登録する（キーチェーン）
  register     初回のデバイス登録のため bark-server を LAN に時間限定で公開する
  doctor       前提条件をチェックする
  test-notify  固定文言の暗号化テスト通知を送る
  inspect      Slack 通知レコードの形式（キー・型・文字数のみ）を表示する
  run          watcher 本体（通常は toggle から起動される）
`

func main() {
	// Everything we and our children (bark-server) create is private (P14).
	syscall.Umask(0o077)

	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	cmds := map[string]func([]string) int{
		"run":         cmdRun,
		"toggle":      cmdToggle,
		"status":      cmdStatus,
		"setup":       cmdSetup,
		"register":    cmdRegister,
		"doctor":      cmdDoctor,
		"test-notify": cmdTestNotify,
		"inspect":     cmdInspect,
	}
	f, ok := cmds[os.Args[1]]
	if !ok {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	os.Exit(f(os.Args[2:]))
}

// app bundles what every subcommand needs.
type app struct {
	cfg   config.Config
	paths config.Paths
	store secrets.Store
	self  string
}

func loadApp() (*app, error) {
	dir, err := config.DefaultDir()
	if err != nil {
		return nil, err
	}
	paths := config.PathsFor(dir, config.Config{})
	cfg, err := config.Load(paths.ConfigFile)
	if err != nil {
		return nil, err
	}
	paths = config.PathsFor(dir, cfg)
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	return &app{cfg: cfg, paths: paths, store: secrets.NewKeychain(), self: self}, nil
}

func (a *app) manager() *procctl.Manager {
	return &procctl.Manager{Paths: a.paths, Cfg: a.cfg, Self: a.self}
}

// notificationDBPath returns the configured or first existing candidate path.
func (a *app) notificationDBPath() (string, error) {
	if a.cfg.NotificationDBPath != "" {
		return a.cfg.NotificationDBPath, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	out, _ := exec.Command("/usr/bin/getconf", "DARWIN_USER_DIR").Output()
	for _, p := range config.NotificationDBCandidates(home, strings.TrimSpace(string(out))) {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", errors.New("通知センター DB が見つかりません")
}

func fail(format string, args ...any) int {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	return 1
}
