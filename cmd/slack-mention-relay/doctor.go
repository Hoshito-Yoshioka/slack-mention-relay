package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"slack-mention-relay/internal/barkclient"
	"slack-mention-relay/internal/config"
	relaycrypto "slack-mention-relay/internal/crypto"
	"slack-mention-relay/internal/notifdb"
	"slack-mention-relay/internal/procctl"
	"slack-mention-relay/internal/redact"
	"slack-mention-relay/internal/secrets"
)

// cmdDoctor checks prerequisites. Output never includes notification
// content or secrets.
func cmdDoctor([]string) int {
	a, err := loadApp()
	if err != nil {
		return fail("%v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	ng := 0
	check := func(name string, ok bool, detail string) {
		mark := "OK"
		if !ok {
			mark = "NG"
			ng++
		}
		fmt.Printf("[%s] %-22s %s\n", mark, name, detail)
	}
	info := func(name, detail string) { fmt.Printf("[--] %-22s %s\n", name, detail) }

	if v, err := exec.Command("/usr/bin/sw_vers", "-productVersion").Output(); err == nil {
		info("macOS", strings.TrimSpace(string(v)))
	}

	// Data directory permissions (P14).
	if fi, err := os.Stat(a.paths.Dir); err != nil {
		check("data dir", false, redact.HomePath(a.paths.Dir)+" がありません（setup を実行）")
	} else {
		warns := config.CheckPerms(a.paths.Dir)
		check("data dir perms", fi.Mode().Perm() == config.DirPerm && len(warns) == 0,
			fmt.Sprintf("%s %04o %s", redact.HomePath(a.paths.Dir), fi.Mode().Perm(), strings.Join(warns, "; ")))
	}

	// Notification DB: location, readability (Full Disk Access), Slack app.
	dbPath, err := a.notificationDBPath()
	if err != nil {
		check("notification DB", false, err.Error())
	} else {
		check("notification DB", true, redact.HomePath(dbPath))
		db, err := notifdb.Open(dbPath, a.cfg.SlackBundleIDs)
		if err == nil {
			err = db.Ping(ctx)
		}
		check("full disk access", err == nil, ifElse(err == nil, "DB を読み取り専用で開けました",
			"DB を読めません。フルディスクアクセスを付与してください（ターミナルから実行した場合はそのターミナルアプリ）"))
		if err == nil {
			ok, err := db.HasApp(ctx)
			check("slack in DB", err == nil && ok, ifElse(ok, strings.Join(a.cfg.SlackBundleIDs, ","),
				"Slack の通知記録がありません（Slack の通知を許可し、一度通知を受けてください）"))
			_ = db.Close()
		}
	}

	// bark-server binary.
	if fi, err := os.Stat(a.paths.BarkServer); err != nil {
		check("bark-server", false, "見つかりません（scripts/install-bark-server.sh を実行）")
	} else {
		mode := fi.Mode().Perm()
		check("bark-server", mode&0o100 != 0 && mode&0o022 == 0,
			fmt.Sprintf("%s %04o", redact.HomePath(a.paths.BarkServer), mode))
	}

	// Keychain items (values are never printed).
	key, err := a.store.Get(ctx, secrets.AccountAESKey)
	check("keychain aes_key", err == nil && relaycrypto.ValidateKey(key) == nil, ifElse(err == nil, "登録済み", "未登録（setup を実行）"))
	dk, err := a.store.Get(ctx, secrets.AccountDeviceKey)
	check("keychain device_key", err == nil && barkclient.ValidateDeviceKey(dk) == nil, ifElse(err == nil, "登録済み", "未登録（setup を実行）"))

	// Listen address: configured value and, if running, the actual sockets.
	addr := a.cfg.LoopbackAddr()
	check("listen addr (config)", procctl.ValidateListenAddr(addr, false) == nil, addr)
	st := a.manager().Status()
	if st.Bark != 0 {
		listens, err := listeningAddrs(st.Bark)
		ok := err == nil && len(listens) > 0
		for _, l := range listens {
			if !isLoopbackHostPort(l) {
				ok = false
			}
		}
		check("listen addr (actual)", ok, strings.Join(listens, ", "))
	} else {
		info("listen addr (actual)", "bark-server 停止中")
	}
	info("relay", ifElse(st.On(), "ON", "OFF"))

	if ng > 0 {
		fmt.Printf("\n%d 件の NG があります。docs/setup.md を参照してください。\n", ng)
		return 1
	}
	fmt.Println("\nすべての前提条件を満たしています。")
	return 0
}

// listeningAddrs lists the TCP listen sockets of pid via lsof.
func listeningAddrs(pid int) ([]string, error) {
	out, err := exec.Command("/usr/sbin/lsof", "-a", "-p", strconv.Itoa(pid), "-iTCP", "-sTCP:LISTEN", "-P", "-n", "-Fn").Output() // #nosec G204 -- numeric pid
	if err != nil && len(out) == 0 {
		return nil, err
	}
	var addrs []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		if l := sc.Text(); strings.HasPrefix(l, "n") {
			addrs = append(addrs, l[1:])
		}
	}
	return addrs, nil
}

func isLoopbackHostPort(hp string) bool {
	host, _, err := net.SplitHostPort(hp)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func ifElse(c bool, a, b string) string {
	if c {
		return a
	}
	return b
}
