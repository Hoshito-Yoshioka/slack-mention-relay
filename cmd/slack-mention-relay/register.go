package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os/signal"
	"regexp"
	"strconv"
	"syscall"
	"time"

	"slack-mention-relay/internal/config"
	"slack-mention-relay/internal/procctl"
)

// Matches bark-server's access log line for a successful registration:
// "<time> INFO <ip> -> [200] POST <latency> /register => <url> <body>".
// The line also contains the device token; it is matched in memory only and
// never printed or stored.
var registeredRe = regexp.MustCompile(`-> \[200\] (?:GET|POST) \S+ /register =>`)

// cmdRegister exposes bark-server on one LAN IP (never 0.0.0.0) for a
// limited time so the Bark app can register this server (section 6.2).
func cmdRegister(args []string) int {
	fs := flag.NewFlagSet("register", flag.ContinueOnError)
	ipFlag := fs.String("ip", "", "待ち受ける LAN IP（省略時は自動検出）")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	a, err := loadApp()
	if err != nil {
		return fail("%v", err)
	}
	if err := config.EnsureDir(a.paths); err != nil {
		return fail("%v", err)
	}
	if s := a.manager().Status(); s.On() || s.Bark != 0 {
		return fail("relay が動作中です。先に toggle で OFF にしてください")
	}

	ip := *ipFlag
	if ip == "" {
		if ip, err = detectLANIP(); err != nil {
			return fail("%v（--ip で指定してください）", err)
		}
	}
	addr := net.JoinHostPort(ip, strconv.Itoa(a.cfg.Port))

	pr, pw := io.Pipe()
	spec, err := procctl.BarkServerSpec(a.paths.BarkServer, addr, a.paths.BarkDataDir, true, pw)
	if err != nil {
		return fail("この IP では待ち受けられません（プライベート IP のみ可）")
	}
	bark, err := procctl.Spawn(spec)
	if err != nil {
		return fail("bark-server を起動できません: %v", err)
	}
	defer procctl.StopChild(bark, 3*time.Second)

	registered := make(chan struct{}, 1)
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() { // keep draining so bark-server never blocks on stdout
			if registeredRe.Match(sc.Bytes()) {
				select {
				case registered <- struct{}{}:
				default:
				}
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), a.cfg.RegisterTimeout())
	defer cancel()
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	wctx, wcancel := context.WithTimeout(ctx, 5*time.Second)
	err = procctl.WaitListening(wctx, addr, func() bool { return !bark.Exited() })
	wcancel()
	if err != nil {
		return fail("bark-server: %v", err)
	}

	fmt.Printf("bark-server を %s で一時的に公開しました（%s 後に自動停止）。\n", addr, a.cfg.RegisterTimeout())
	fmt.Println("1. iPhone を同じ Wi-Fi に接続する")
	fmt.Printf("2. Bark アプリ → サーバー → 追加 で http://%s を入力する\n", addr)
	fmt.Println("3. 登録を検知したら自動で停止します（Ctrl-C で中止）")

	select {
	case <-registered:
		// Let the app receive its response before shutting down.
		time.Sleep(time.Second)
		fmt.Println()
		fmt.Println("✅ 登録を検知しました。bark-server を停止します。")
		fmt.Println("次の手順:")
		fmt.Println("  - Bark アプリに表示された device_key を `slack-mention-relay setup` で入力する")
		fmt.Println("  - Bark アプリから公式サーバー（api.day.app）を削除する（自前サーバーを追加した後に）")
		return 0
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fail("時間切れです。登録は検知されませんでした")
		}
		return fail("中止しました")
	}
}

// detectLANIP returns the first private IPv4 address of an up, non-loopback
// interface, preferring en0.
func detectLANIP() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	var candidates []string
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, ad := range addrs {
			ipn, ok := ad.(*net.IPNet)
			if !ok || ipn.IP.To4() == nil || !ipn.IP.IsPrivate() {
				continue
			}
			if ifc.Name == "en0" {
				return ipn.IP.String(), nil
			}
			candidates = append(candidates, ipn.IP.String())
		}
	}
	if len(candidates) == 0 {
		return "", errors.New("LAN の IP アドレスが見つかりません")
	}
	return candidates[0], nil
}
