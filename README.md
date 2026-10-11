# slack-mention-relay

**Mac の Slack に届いたメンションを、離席中だけ iPhone に通知するツールです。**

Slack のモバイルアプリが使えない環境では、会社の Mac の前を離れているあいだに届いたメンションに気づけません。たとえば、ワークスペースのポリシーでモバイルのセッションが切れる場合です。

slack-mention-relay は、Mac で動いている Slack デスクトップアプリの通知を拾い、iPhone に転送します。

1. 席を離れるときに、Hotkey を押して **ON** にする
2. 離席中に自分宛てのメンションや DM が届くと、iPhone に「どのチャンネルで、誰から、どんなメッセージか」が通知される
3. 席に戻ったら、もう一度 Hotkey を押して **OFF** にする（在席中は通知しない）

業務の情報を扱うことを前提に、次の方針で作っています。

- **第三者のサーバーを経由しない。** 通知は Mac 上で動く bark-server から Apple のプッシュ通知サービス（APNs）を通って iPhone に届きます。それ以外のサーバーは通りません
- **通知の中身は暗号化する。** Mac で暗号化し、iPhone の上で初めて復号します。APNs を通るときも暗号文のままです
- **Slack App を作らない。** Slack の API もトークンも使わないので、ワークスペース管理者の承認は不要です
- **無料で使える。** 有料のサービスやアプリは使いません

> このツールの安全性は、設計を秘密にすることには依存していません。暗号鍵（macOS のキーチェーンにだけ保存）と、ネットワークに公開しない設計（ループバック限定）に依存しています。詳しくは [docs/threat-model.md](docs/threat-model.md) を参照してください。

## 仕組み

```
[Slack デスクトップ] ─通知→ macOS 通知センターの DB
                                │ 数秒おきに読み取る（読み取り専用）
                                ▼
                    slack-mention-relay（AES-256-GCM で暗号化）
                                │ 127.0.0.1 のみ
                                ▼
                    bark-server（Mac 上で動く OSS）
                                │ HTTPS
                                ▼
                    Apple APNs ─→ iPhone の Bark アプリ（端末上で復号して表示）
```

iPhone 側の通知アプリには、オープンソースの [Bark](https://github.com/Finb/Bark) を使います。中継サーバーには、同じ作者の [bark-server](https://github.com/Finb/bark-server) をバージョンと SHA256 を固定して使います。

## 動作条件と制約

- macOS 専用です（Apple Silicon / Intel）。Mac は起動したままにし、Slack デスクトップアプリも動かしておく必要があります。ON の間、本ツールはシステムスリープだけを防ぎます。画面ロックとディスプレイのスリープは通常どおり動作します
- macOS の通知センター DB を読んでいます。DB は Apple が公開している仕様ではないため、macOS や Slack のアップデートで動かなくなる可能性があります
- 想定している使い方は、Mac 1 台と iPhone 1 台の個人利用です
- 本プロジェクトは、Slack、Apple、Bark とは関係のない非公式のツールです

## 必要なもの

- macOS と Go 1.26 以上（ビルドに使います）
- [Raycast](https://www.raycast.com/)（無料。Hotkey での ON/OFF に使います）
- iPhone の Bark アプリ：[App Store の Bark - Custom Notifications](https://apps.apple.com/app/bark-custom-notifications/id1403753865)（無料。デベロッパ名は「丰 黄」です。似た名前のアプリがあるので、このリンクから入れてください）
- Mac と iPhone を同じ Wi-Fi につなげる環境（初回の登録時だけ必要です）

## はじめかた

手順の全体は [docs/setup.md](docs/setup.md) にあります。おおまかな流れは次のとおりです。

```bash
# 1. ビルドと bark-server の取得（バージョンと SHA256 を固定）
go build -o ~/.slack-relay/bin/slack-mention-relay ./cmd/slack-mention-relay && chmod 700 ~/.slack-relay/bin/slack-mention-relay
scripts/install-bark-server.sh

# 2. iPhone の Bark アプリをこの Mac に登録する（LAN 限定・最大 5 分）
~/.slack-relay/bin/slack-mention-relay register

# 3. 暗号鍵の生成と device_key の登録（どちらもキーチェーンに保存）
~/.slack-relay/bin/slack-mention-relay setup

# 4. 確認
~/.slack-relay/bin/slack-mention-relay test-notify
~/.slack-relay/bin/slack-mention-relay doctor
```

最後に Raycast で、このリポジトリの `raycast/` を Script Command として追加し、`Slack Relay Toggle` に Hotkey を割り当てます。Hotkey を押すたびに、`🔔 Slack relay ON` / `🔕 Slack relay OFF` が表示されます。

## コマンド

| コマンド      | 内容                                                                                                                       |
| ------------- | -------------------------------------------------------------------------------------------------------------------------- |
| `toggle`      | ON/OFF を切り替える（Raycast から呼ばれる）                                                                                |
| `status`      | 現在の状態を表示する                                                                                                       |
| `setup`       | 暗号鍵の生成（`--rotate-key` で作り直し）と device_key の登録                                                              |
| `register`    | 初回のデバイス登録のため、bark-server を LAN に時間限定で公開する                                                          |
| `doctor`      | 前提条件（フルディスクアクセス、鍵、待ち受けアドレスなど）をチェックする                                                   |
| `test-notify` | 固定の文言で暗号化テスト通知を送る（`--wrong-key` で復号に失敗する場合を確認できる）                                       |
| `inspect`     | Slack 通知の記録の形式を表示する。キー・型・文字数だけで、値は表示しない（[docs/investigation.md](docs/investigation.md)） |
| `run`         | 監視プロセス本体（通常は `toggle` から起動される）                                                                         |

## 設定とデータの保存場所

- 実行時のデータ（ログ、pid ファイル、bark-server のデータ）は `~/.slack-relay/` に置きます。権限はディレクトリが `0700`、ファイルが `0600` です
- 設定ファイルは任意です。`~/.slack-relay/config.json` に置きます（[configs/config.example.json](configs/config.example.json) を参照）。設定できるのは、ポート、ポーリング間隔などです
- 暗号鍵と device_key は、macOS のキーチェーンにだけ保存します。設定ファイルや環境変数には置きません
- ログには、チャンネル名・送信者・本文・鍵を出しません

## 依存関係

依存は最小限にしています。

| ライブラリ           | 用途                                               |
| -------------------- | -------------------------------------------------- |
| `modernc.org/sqlite` | 通知センター DB を読む（cgo 不要の純 Go 実装）     |
| `howett.net/plist`   | 通知の記録（binary plist）を解析する               |
| `golang.org/x/term`  | `setup` で device_key を画面に表示せずに入力させる |

外部プログラムとして、[bark-server](https://github.com/Finb/bark-server)、`/usr/bin/security`（キーチェーン）、`/usr/bin/caffeinate`（スリープ防止）を使います。

## コントリビュート

Issue や Pull Request を歓迎します。

```bash
go test ./...
scripts/install-hooks.sh   # コミット前に gitleaks で秘密情報を検査する
```

CI（GitHub Actions）では、build / vet / test / staticcheck / gosec / govulncheck / gitleaks / shellcheck を実行します。

**Issue や PR にログや出力を貼るときの注意**

- ログは内容をマスクしていますが、貼る前に必ず目で確認してください
- 通知センター DB の中身、`~/.slack-relay/` の中のファイル、`security` コマンドの出力は貼らないでください
- ワークスペース名、ユーザー名、LAN の IP アドレス、ホスト名、`/Users/<名前>` のようなパスは伏せてください
- `inspect` の出力には値が含まれないので、そのまま貼って問題ありません

**コミットの作者情報**

個人のメールアドレスが公開されないよう、GitHub の noreply アドレスの利用をおすすめします（`git config user.email "<ID>+<ユーザー名>@users.noreply.github.com"`）。

**フォークして公開する場合**

GitHub の Settings → Code security で、次を有効にしてください。

- Secret scanning
- Push protection
- Private vulnerability reporting
- Dependabot alerts

## セキュリティ

脆弱性を見つけた場合は、公開の Issue ではなく [SECURITY.md](SECURITY.md) の方法で報告してください。設計上の前提と残るリスクは、[docs/threat-model.md](docs/threat-model.md) にまとめています。

## ライセンス

[MIT](LICENSE)
