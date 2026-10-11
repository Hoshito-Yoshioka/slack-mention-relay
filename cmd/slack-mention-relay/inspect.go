package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"slack-mention-relay/internal/notifdb"
)

// cmdInspect prints the structure (key paths, types, lengths) of the newest
// Slack notification records. Values are never printed, so the output can
// be shared when investigating the notification format (docs/investigation.md).
func cmdInspect(args []string) int {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	n := fs.Int("n", 3, "表示する件数（新しい順）")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *n < 1 || *n > 20 {
		return fail("-n は 1〜20 で指定してください")
	}
	a, err := loadApp()
	if err != nil {
		return fail("%v", err)
	}
	dbPath, err := a.notificationDBPath()
	if err != nil {
		return fail("%v", err)
	}
	db, err := notifdb.Open(dbPath, a.cfg.SlackBundleIDs)
	if err != nil {
		return fail("%v", err)
	}
	defer func() { _ = db.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	recs, err := db.Latest(ctx, *n)
	if err != nil {
		return fail("通知 DB を読めません（フルディスクアクセスを確認）")
	}
	if len(recs) == 0 {
		return fail("Slack の通知記録がありません")
	}
	fmt.Println("# 値は表示していません（キー・型・文字数のみ）。この出力はそのまま共有できます。")
	for i, r := range recs {
		fmt.Printf("\n--- %d 件目（新しい順） %s前\n", i+1, time.Since(r.Delivered).Round(time.Second))
		lines, err := notifdb.Describe(r.Data)
		if err != nil {
			fmt.Println("(plist を解析できません)")
			continue
		}
		for _, l := range lines {
			fmt.Println(l)
		}
	}
	return 0
}
