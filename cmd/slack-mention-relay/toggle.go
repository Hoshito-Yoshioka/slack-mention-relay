package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"slack-mention-relay/internal/config"
	"slack-mention-relay/internal/procctl"
)

const (
	hudOn   = "🔔 Slack relay ON"
	hudOff  = "🔕 Slack relay OFF"
	hudFail = "⚠️ 起動失敗: "
)

// cmdToggle prints exactly one line for Raycast's HUD and always exits 0 so
// that the HUD (not an error toast) shows the message.
func cmdToggle([]string) int {
	fmt.Println(toggle())
	return 0
}

func toggle() string {
	a, err := loadApp()
	if err != nil {
		return hudFail + summarize(err)
	}
	if err := config.EnsureDir(a.paths); err != nil {
		return hudFail + summarize(err)
	}
	unlock, err := procctl.Lock(filepath.Join(a.paths.Dir, "toggle.lock"))
	if err != nil {
		return hudFail + "切り替え中です"
	}
	defer unlock()

	m := a.manager()
	if m.Status().On() {
		if err := m.Off(); err != nil {
			return "⚠️ 停止失敗: " + summarize(err)
		}
		return hudOff
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := m.On(ctx); err != nil {
		return hudFail + summarize(err)
	}
	return hudOn
}

// summarize keeps the HUD to one short line.
func summarize(err error) string {
	s := strings.Join(strings.Fields(err.Error()), " ")
	if utf8.RuneCountInString(s) > 80 {
		s = string([]rune(s)[:80]) + "…"
	}
	return s
}

func cmdStatus([]string) int {
	a, err := loadApp()
	if err != nil {
		return fail("%v", err)
	}
	s := a.manager().Status()
	state := "OFF"
	if s.On() {
		state = "ON"
	}
	fmt.Printf("relay:        %s\n", state)
	fmt.Printf("watcher:      %s\n", pidText(s.Run))
	fmt.Printf("bark-server:  %s (%s)\n", pidText(s.Bark), a.cfg.LoopbackAddr())
	fmt.Printf("caffeinate:   %s\n", pidText(s.Caffeinate))
	if s.On() && (s.Bark == 0 || s.Caffeinate == 0) {
		fmt.Println("warning: 一部のプロセスが停止しています。toggle で OFF→ON し直してください")
	}
	return 0
}

func pidText(pid int) string {
	if pid == 0 {
		return "stopped"
	}
	return fmt.Sprintf("running (pid %d)", pid)
}
