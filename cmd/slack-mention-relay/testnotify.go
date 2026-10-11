package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"slack-mention-relay/internal/barkclient"
	"slack-mention-relay/internal/config"
	relaycrypto "slack-mention-relay/internal/crypto"
	"slack-mention-relay/internal/procctl"
	"slack-mention-relay/internal/redact"
	"slack-mention-relay/internal/secrets"
	"slack-mention-relay/internal/watcher"
)

// testMessage contains no workspace information.
var testMessage = barkclient.Message{
	Title:    "slack-mention-relay",
	Subtitle: "テスト",
	Body:     "暗号化テスト通知です。この文面が読めれば復号に成功しています。",
	Group:    watcher.Group,
}

// cmdTestNotify sends a fixed, encrypted test message. If bark-server is not
// running it is started on loopback for the duration of the test.
func cmdTestNotify(args []string) int {
	fs := flag.NewFlagSet("test-notify", flag.ContinueOnError)
	wrongKey := fs.Bool("wrong-key", false, "わざと別の鍵で暗号化する（iPhone に Decryption Failed が出れば正常）")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	a, err := loadApp()
	if err != nil {
		return fail("%v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	key, err1 := a.store.Get(ctx, secrets.AccountAESKey)
	dk, err2 := a.store.Get(ctx, secrets.AccountDeviceKey)
	if err1 != nil || err2 != nil {
		return fail("キーチェーンに鍵がありません（setup を実行）")
	}
	if *wrongKey {
		if key, err = relaycrypto.GenerateKey(); err != nil {
			return fail("%v", err)
		}
	}

	addr := a.cfg.LoopbackAddr()
	if a.manager().Status().Bark == 0 {
		if err := config.EnsureDir(a.paths); err != nil {
			return fail("%v", err)
		}
		spec, err := procctl.BarkServerSpec(a.paths.BarkServer, addr, a.paths.BarkDataDir, false, nil)
		if err != nil {
			return fail("%v", err)
		}
		bark, err := procctl.Spawn(spec)
		if err != nil {
			return fail("bark-server を起動できません: %v", err)
		}
		defer procctl.StopChild(bark, 3*time.Second)
		wctx, wcancel := context.WithTimeout(ctx, 5*time.Second)
		err = procctl.WaitListening(wctx, addr, func() bool { return !bark.Exited() })
		wcancel()
		if err != nil {
			return fail("bark-server: %v", err)
		}
	}

	client, err := barkclient.New(addr, dk, key)
	if err != nil {
		return fail("%v", err)
	}
	if err := client.Send(ctx, testMessage); err != nil {
		return fail("送信に失敗しました: %s", redact.ErrKind(err))
	}
	if *wrongKey {
		fmt.Println("送信しました。iPhone に「Decryption Failed」と表示されれば正常です。")
	} else {
		fmt.Println("送信しました。iPhone に「暗号化テスト通知です…」と表示されれば成功です。")
	}
	return 0
}
