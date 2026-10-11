package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"slack-mention-relay/internal/barkclient"
	"slack-mention-relay/internal/config"
	"slack-mention-relay/internal/notifdb"
	"slack-mention-relay/internal/redact"
	"slack-mention-relay/internal/secrets"
	"slack-mention-relay/internal/watcher"
)

// cmdRun is the watcher. Logs go to stderr (the log file when started by
// toggle). With --ready-fd, the startup result is reported on that fd.
func cmdRun(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	readyFD := fs.Int("ready-fd", 0, "report startup status on this fd (used by toggle)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	report := func(string) {}
	if *readyFD > 2 {
		f := os.NewFile(uintptr(*readyFD), "ready")
		report = func(line string) {
			fmt.Fprintln(f, line)
			_ = f.Close()
			report = func(string) {}
		}
	}
	failStart := func(reason string) int {
		report("error: " + reason)
		fmt.Fprintln(os.Stderr, "error: "+reason)
		return 1
	}

	a, err := loadApp()
	if err != nil {
		return failStart("設定を読めません")
	}
	log := redact.New(os.Stderr, a.cfg.Debug)
	for _, w := range config.CheckPerms(a.paths.Dir) {
		log.Warn("loose permissions", "path", w)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	signal.Ignore(syscall.SIGHUP)

	key, err1 := a.store.Get(ctx, secrets.AccountAESKey)
	dk, err2 := a.store.Get(ctx, secrets.AccountDeviceKey)
	if err1 != nil || err2 != nil {
		return failStart("キーチェーンに鍵がありません（setup を実行）")
	}
	client, err := barkclient.New(a.cfg.LoopbackAddr(), dk, key)
	if err != nil {
		return failStart("鍵または送信先の形式が不正です（setup / doctor を実行）")
	}
	dbPath, err := a.notificationDBPath()
	if err != nil {
		return failStart("通知センター DB が見つかりません")
	}

	w := &watcher.Watcher{
		Open: func() (watcher.Source, func(), error) {
			db, err := notifdb.Open(dbPath, a.cfg.SlackBundleIDs)
			if err != nil {
				return nil, nil, err
			}
			return db, func() { _ = db.Close() }, nil
		},
		Sender:   client,
		Log:      log,
		Interval: a.cfg.PollInterval(),
	}
	if err := w.Init(ctx, time.Now()); err != nil {
		log.Error("preflight failed", "err", err)
		return failStart("通知 DB を読めません（フルディスクアクセスを確認）")
	}
	report("ready")
	log.Info("started", "interval", a.cfg.PollInterval().String())
	err = w.Run(ctx)
	log.Info("stopped")
	if err != nil {
		return 1
	}
	return 0
}
