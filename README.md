# slack-mention-relay

> **このツールの安全性は、設計を秘密にすることには依存していません。**
> 端末間の暗号鍵（キーチェーンにだけ保存）と、ループバック限定の設計に依存しています。
> そのためソースコードは公開して問題ありません。

Mac 上の Slack デスクトップアプリが受け取ったメンション通知を、**ON にしている間だけ** iPhone に転送する個人用ツールです。

```
[Slack デスクトップ] → macOS 通知センター DB（読み取り専用）
        → slack-mention-relay run（AES-256-GCM で暗号化）
        → bark-server（127.0.0.1 でのみ待ち受け）
        → Apple APNs → iPhone の Bark アプリ（端末上で復号）
```

- Apple の APNs 以外の第三者サーバーは経由しません
- 通知内容は APNs を通る時点でも暗号文です
- Slack App の作成・インストールは不要です
- ON/OFF は Raycast の Hotkey 1 つで切り替えます。ON の間はシステムスリープだけを防ぎます

## 必要なもの

- macOS（Apple Silicon または Intel）、Go 1.26 以上
- [Raycast](https://www.raycast.com/)
- iPhone の Bark アプリ：[App Store の Bark - Custom Notifications](https://apps.apple.com/app/bark-custom-notifications/id1403753865)（無料。デベロッパ名「丰 黄」、ソースは [Finb/Bark](https://github.com/Finb/Bark)）。入れ方は [docs/setup.md](docs/setup.md) の「0.」

## クイックスタート

詳しい手順は [docs/setup.md](docs/setup.md) を参照してください。

```bash
go build -o ~/.slack-relay/bin/slack-mention-relay ./cmd/slack-mention-relay && chmod 700 ~/.slack-relay/bin/slack-mention-relay
scripts/install-bark-server.sh          # バージョンと SHA256 を固定して取得
~/.slack-relay/bin/slack-mention-relay register   # 初回のデバイス登録（LAN・5 分限定）
~/.slack-relay/bin/slack-mention-relay setup      # AES 鍵の生成と device_key の登録
~/.slack-relay/bin/slack-mention-relay test-notify
~/.slack-relay/bin/slack-mention-relay doctor
```

その後、Raycast で `raycast/` ディレクトリを Script Command として追加し、`Slack Relay Toggle` に Hotkey を割り当てます。

## サブコマンド

| コマンド | 内容 |
|---|---|
| `toggle` | ON/OFF を切り替え、HUD 用の 1 行を出力する |
| `status` | 現在の状態を表示する |
| `setup` | AES 鍵の生成（`--rotate-key` で作り直し）と device_key の登録 |
| `register` | 初回のデバイス登録用に bark-server を LAN IP で時間限定で起動する |
| `doctor` | 前提条件をチェックする |
| `test-notify` | 固定文言の暗号化テスト通知を送る（`--wrong-key` で復号失敗の確認） |
| `inspect` | Slack 通知レコードの形式（キー・型・文字数のみ。値は出さない）を表示する。通知形式の調査用（[docs/investigation.md](docs/investigation.md)） |
| `run` | watcher 本体（通常は `toggle` から起動される） |

実行時のデータは `~/.slack-relay/`（`0700`）に置きます。設定は `~/.slack-relay/config.json`（任意、[configs/config.example.json](configs/config.example.json) を参照）です。秘密情報は設定ファイルには書かず、macOS キーチェーンにだけ保存します。

## 依存ライブラリ

依存は最小限にしています。

| ライブラリ | 理由 |
|---|---|
| `modernc.org/sqlite` | 通知センター DB を読むため。cgo 不要の純 Go 実装 |
| `howett.net/plist` | 通知レコードの binary plist をパースするため |
| `golang.org/x/term` | `setup` で device_key を画面に表示せずに入力させるため（Go 公式の拡張パッケージ） |

外部プログラムとして、上流の [bark-server](https://github.com/Finb/bark-server)（バージョンと SHA256 を固定）、`/usr/bin/security`、`/usr/bin/caffeinate` を使います。

## 開発

```bash
go test ./...
scripts/install-hooks.sh   # pre-commit で gitleaks を実行する
```

CI（GitHub Actions）では、build / vet / test / staticcheck / gosec / govulncheck / gitleaks（全履歴）/ shellcheck を実行します。

### コミットの作者情報

個人のメールアドレスを公開しないよう、GitHub の noreply アドレスを使ってください。

```bash
git config user.email "<ID>+<ユーザー名>@users.noreply.github.com"
```

GitHub の Settings → Emails で「Keep my email addresses private」と「Block command line pushes that expose my email」を有効にしてください。

### Issue や PR にログを貼るときの注意

- ログは既定で内容（チャンネル名・送信者・本文・鍵）をマスクしますが、貼る前に必ず目視で確認してください
- 通知センター DB の中身、`~/.slack-relay/` 配下のファイル、`security` コマンドの出力は貼らないでください
- ワークスペース名、ユーザー名、LAN の IP、ホスト名、`/Users/<名前>` を含む情報は伏せてください

## リポジトリ設定（公開時）

GitHub の Settings → Code security で、次を有効にしてください。

- Secret scanning
- Push protection
- Private vulnerability reporting
- Dependabot alerts / security updates

## セキュリティ

脆弱性の報告方法と、鍵が漏洩したときの対応手順は [SECURITY.md](SECURITY.md) に、脅威モデルは [docs/threat-model.md](docs/threat-model.md) にあります。

## ライセンス

MIT
