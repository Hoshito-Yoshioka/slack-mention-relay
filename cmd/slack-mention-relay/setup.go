package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"slack-mention-relay/internal/barkclient"
	"slack-mention-relay/internal/config"
	relaycrypto "slack-mention-relay/internal/crypto"
	"slack-mention-relay/internal/secrets"
)

// cmdSetup generates the AES key (shown once on the terminal, never logged)
// and stores the device_key. Both live only in the keychain.
func cmdSetup(args []string) int {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	rotate := fs.Bool("rotate-key", false, "AES 鍵を作り直す（Bark アプリ側の再設定が必要）")
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
	ctx := context.Background()

	// 1. AES key.
	_, err = a.store.Get(ctx, secrets.AccountAESKey)
	switch {
	case err == nil && !*rotate:
		fmt.Println("AES 鍵: 登録済み（再表示はしません。作り直す場合は --rotate-key）")
	case err == nil || errors.Is(err, secrets.ErrNotFound):
		key, err := relaycrypto.GenerateKey()
		if err != nil {
			return fail("鍵を生成できません: %v", err)
		}
		if err := a.store.Set(ctx, secrets.AccountAESKey, key); err != nil {
			return fail("キーチェーンに保存できません: %v", err)
		}
		fmt.Println("AES 鍵を生成してキーチェーンに保存しました。")
		fmt.Println("Bark アプリ → 設定 → 暗号化設定 で、次の値を入力してください:")
		fmt.Println("  アルゴリズム: AES256 / モード: GCM / パディング: noPadding")
		fmt.Println("  Key: " + key)
		fmt.Println("  IV:  空欄で可。入力必須なら任意の英数字 12 文字（送信ごとのランダムな iv が優先されます）")
		fmt.Println("※ この鍵はここで一度だけ表示します。メモやスクリーンショットに残さないでください。")
	default:
		return fail("キーチェーンを読めません: %v", err)
	}

	// 2. device_key.
	fmt.Println()
	_, err = a.store.Get(ctx, secrets.AccountDeviceKey)
	hasDK := err == nil
	prompt := "Bark アプリに表示された device_key を入力してください（入力は表示されません）: "
	if hasDK {
		prompt = "device_key は登録済みです。変更する場合は入力、そのままなら Enter: "
	}
	dk, err := readSecret(prompt)
	if err != nil {
		return fail("入力を読めません: %v", err)
	}
	if dk == "" {
		if !hasDK {
			return fail("device_key が入力されていません")
		}
		fmt.Println("device_key: 変更なし")
		return 0
	}
	if err := barkclient.ValidateDeviceKey(dk); err != nil {
		return fail("device_key の形式が不正です")
	}
	if err := a.store.Set(ctx, secrets.AccountDeviceKey, dk); err != nil {
		return fail("キーチェーンに保存できません: %v", err)
	}
	fmt.Println("device_key をキーチェーンに保存しました。次は `slack-mention-relay test-notify` で確認してください。")
	return 0
}

// readSecret reads one line without echo when stdin is a terminal.
func readSecret(prompt string) (string, error) {
	fmt.Print(prompt)
	fd := int(os.Stdin.Fd()) // #nosec G115 -- small fd
	if term.IsTerminal(fd) {
		b, err := term.ReadPassword(fd)
		fmt.Println()
		return strings.TrimSpace(string(b)), err
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}
