# slack-mention-relay 要件定義書（Claude Code 向け）

Mac mini 上の Slack デスクトップアプリが受け取ったメンション通知を、離席中だけ iPhone に転送する個人用ツール。
本書の目的は、実装担当（Claude Code）が追加の質問なしに実装を始められるようにすること。
未確定事項は「10. 未確定事項」にまとめてある。実装の最初のステップで調査すること。

- リポジトリ名：`slack-mention-relay`
- バイナリ名：`slack-mention-relay`
- 言語：**Go**（バックエンドのロジックはすべて Go で書く）
- **公開リポジトリとして運用しても安全な作りにすること**（詳細は 8 章）

---

## 1. 目的

- 業務 Slack の iPhone アプリは、ワークスペースのポリシーでセッションが切れるため使えない
- Mac mini の前にいないとき（離席中）に、Slack のメンションを iPhone で受け取りたい
- 在席中は通知はいらない（うるさいため）。ツールを ON にしている間だけ通知する

## 2. 絶対条件（変更不可）

| # | 条件 | 理由 |
|---|------|------|
| C1 | **完全無料**で実現する | 有料のサービスやアプリは使わない |
| C2 | **Apple の APNs 以外の第三者サーバーを経由しない** | 業務情報を扱うため。ntfy.sh、Pushover、Bark 公式サーバー（api.day.app）などは使用不可 |
| C3 | 通知内容は**端末間で暗号化**し、APNs を通る時点でも平文にしない | 同上 |
| C4 | **Slack App を作成・インストールしない** | ワークスペースのアプリ追加が管理者承認制のため |
| C5 | **追加の Apple ID を作らない** | ユーザーの要望 |
| C6 | Gmail などのメールは使わない | 受信箱を汚したくない |
| C7 | 実行環境はユーザーの Mac mini（ローカル）のみ | デプロイは不要 |
| C8 | ロジックは Go で実装する。Raycast 連携とセットアップ補助には bash のシェルスクリプトを使ってよい | ユーザーの指定（シェルスクリプトの使用は承認済み） |
| C9 | 公開リポジトリにしても、秘密情報・個人情報・業務情報が漏れない | 8 章を参照 |

## 3. 機能要件

### F1. ON/OFF の切り替え（Raycast）
- Raycast の Script Command を 1 つだけ用意し、Hotkey で実行できるようにする
- 実行するたびに ON と OFF が切り替わる
  - OFF から ON にするとき：必要なプロセスをすべて起動する（事前に「アプリを起動する」手順は不要）
  - ON から OFF にするとき：すべてのプロセスを停止する
- 切り替え結果を Raycast の HUD で表示する（`🔔 Slack relay ON` / `🔕 Slack relay OFF` / `⚠️ 起動失敗: ...`）
- ターミナル操作は一切不要にすること
- Script Command は Go バイナリの `toggle` サブコマンドを呼ぶだけの薄いラッパーにする（ロジックは Go 側に置く）

### F2. メンションの検知
- Mac 上の Slack デスクトップアプリが出した macOS 通知を、macOS 通知センターの SQLite DB から読み取る
- **ON にした時刻より後に**記録された通知だけを対象にする
- 同じ通知を二重に送らない（レコード ID で重複を除く）
- ポーリング間隔は 3〜5 秒を目安にする（設定で変更できるようにする）

### F3. iPhone への通知
- 通知には次の 3 点を含める
  1. どのチャンネルか（DM の場合はその旨）
  2. 誰からか
  3. どんなメッセージか（本文）
- 送信経路：本ツール → 自前の bark-server（Mac mini 上） → Apple APNs → iPhone の Bark アプリ
- 通知内容はすべて AES の暗号文として送る（6.3 節を参照）

### F4. スリープの制御
- ON の間は、**システムスリープだけを防ぐ**。ディスプレイスリープと画面ロックは通常どおり動作させる
- OFF にした瞬間に、通常のスリープ動作に戻ること
- 実装方法：`caffeinate -i -w <watcherのPID>` を使う（`-d` や `-u` は付けない）

## 4. システム構成

```
[Slack デスクトップ] → macOS 通知センター DB (SQLite)
                              │ 数秒おきに読み取る（読み取り専用）
                              ▼
          [slack-mention-relay run（Go / watcher）]
                              │ AES-256-GCM で暗号化
                              │ HTTP POST http://127.0.0.1:<port>/<device_key>
                              ▼
          [bark-server（OSS / 127.0.0.1 でのみ待ち受け）]
                              │ HTTPS (api.push.apple.com)
                              ▼
                        [Apple APNs] → [iPhone: Bark アプリの中で復号して表示]

[Raycast の Hotkey] → raycast/toggle.sh → slack-mention-relay toggle
                         → bark-server / watcher / caffeinate をまとめて起動・停止
```

bark-server は自作せず、上流（Finb/bark-server）のリリース版を使う。
理由：APNs への送信部分の検証が済んでいること、APNs 送信用の鍵を本リポジトリに含めずに済むこと。

## 5. リポジトリ構成

```
slack-mention-relay/
├── cmd/slack-mention-relay/main.go   # サブコマンドの入口
├── internal/
│   ├── notifdb/      # 通知 DB の読み取りとパース
│   ├── crypto/       # Bark 互換の AES-256-GCM 暗号化
│   ├── barkclient/   # bark-server への送信（127.0.0.1 のみ）
│   ├── secrets/      # macOS キーチェーンへのアクセス
│   ├── procctl/      # pid 管理、起動・停止、caffeinate
│   ├── config/       # 秘密ではない設定（ポート、ポーリング間隔など）
│   └── redact/       # ログのマスキング
├── raycast/toggle.sh                 # Raycast Script Command（数行のラッパー）
├── scripts/
│   ├── install-bark-server.sh        # バージョン固定＋SHA256 検証付きでダウンロードする
│   └── install-hooks.sh              # pre-commit フック（gitleaks）を設定する
├── configs/config.example.json       # プレースホルダーだけを書いた設定例
├── docs/
│   ├── setup.md                      # セットアップと初回登録の手順
│   ├── investigation.md              # 通知 DB の調査メモ（実データは書かない）
│   └── threat-model.md               # 脅威モデルと残るリスク
├── testdata/                         # 合成データだけを置く（実データ禁止）
├── .github/
│   ├── workflows/ci.yml
│   └── dependabot.yml
├── .gitignore
├── .gitleaks.toml
├── go.mod / go.sum
├── LICENSE                           # MIT を推奨
├── SECURITY.md
└── README.md
```

実行時のデータは `~/.slack-relay/` に置く（pid ファイル、bark-server の DB、ログ、設定）。
ディレクトリの権限は `0700`、ファイルの権限は `0600` にする。

### サブコマンド

| コマンド | 内容 |
|---|---|
| `run` | watcher 本体（前面で動く。`toggle` から切り離して起動される） |
| `toggle` | ON/OFF を切り替えて、HUD 用の 1 行を標準出力に出す |
| `status` | 現在の状態を表示する |
| `setup` | AES 鍵の生成、device_key の登録（キーチェーンへ保存） |
| `register` | 初回のデバイス登録のために、bark-server を LAN 向けに時間限定で起動する（6.2 節） |
| `doctor` | 前提条件をチェックする（フルディスクアクセス、DB の場所、bark-server、キーチェーン、待ち受けアドレス） |
| `test-notify` | 固定の文言（業務情報を含まない）で暗号化テスト通知を送る |

## 6. コンポーネントの詳細

### 6.1 bark-server
- `scripts/install-bark-server.sh` で GitHub Releases から **darwin_arm64**（Intel Mac なら amd64）のバイナリを取得する
  - **バージョンを固定し、SHA256 チェックサムを検証する**。検証に失敗したら中止する
  - バイナリはリポジトリにコミットしない（`.gitignore` で除外する）
- 通常運用時の起動オプション
  - `--addr 127.0.0.1:<port>`（**必須**。デフォルトの `0.0.0.0:8080` は LAN 全体に公開されてしまう）
  - `--data ~/.slack-relay/bark-data`
- **標準出力のアクセスログは捨てる**（`/dev/null`）。上流のソース（router.go）を確認したところ、アクセスログに URL とリクエストボディが出力されていた
- `procctl` は bark-server を起動するとき、待ち受けアドレスがループバック以外になっていれば起動を拒否する（`register` コマンドを除く）
- 上流のソースを確認した結果、外向きの通信先は `api.push.apple.com` だけだった

### 6.2 初回のデバイス登録（`register` サブコマンドと docs/setup.md）
iPhone の Bark アプリが bark-server に接続するのは、デバイスを登録するときだけ。

1. `slack-mention-relay register` を実行する
   - Mac の LAN IP を指定して bark-server を起動する（`0.0.0.0` は使わない）
   - **5 分たったら自動で停止する**。登録が成功したら、その時点で停止する
   - Bark アプリのソースでは Basic 認証への対応を確認できなかったので、認証ではなく「LAN 限定・時間限定」で守る
2. iPhone を同じ Wi-Fi に接続し、Bark アプリで表示された URL をサーバーとして追加する（Bark アプリが HTTP 接続を許可していることは確認済み）
3. アプリに表示された device_key を `setup` で入力し、キーチェーンに保存する
4. **Bark アプリから公式サーバー（api.day.app）を削除する**
   - サーバーが 0 件になると公式サーバーが自動で再追加される仕様なので、必ず「自前サーバーを追加した後に」削除する
5. 以後、bark-server は 127.0.0.1 でだけ起動する
6. iPhone の機種変更やアプリの再インストールをしたときは、この手順をやり直す

### 6.3 暗号化の仕様（Bark アプリのソースで確認した仕様に合わせる）
- アルゴリズム：**AES-256 / GCM / noPadding**。Go 標準の `crypto/aes` と `crypto/cipher`（`cipher.NewGCM`）を使う
- 鍵：**ASCII 32 文字**のランダムな英数字。`setup` で `crypto/rand` を使って生成し、キーチェーンに保存する。同時に、Bark アプリの暗号化設定に入力するため画面に一度だけ表示する（ログには出さない）
- IV：**メッセージごとに新しいランダムな ASCII 12 文字**を `crypto/rand` で生成し、`iv` パラメータとして平文で送る。GCM で IV を再利用するのは厳禁
- 暗号化する中身：JSON 文字列。キーは `title` / `subtitle` / `body` / `group`
- 出力：`base64(暗号文 || 認証タグ16バイト)`。Go の `gcm.Seal()` の出力形式は、CryptoSwift の GCM `.combined` モードと一致するはず。**実機で復号できるか必ず確認する**
- 送信：`POST http://127.0.0.1:<port>/<device_key>`。フォームパラメータは `ciphertext` と `iv` **だけ**
  - `title` / `body` / `group` などを平文パラメータとして**絶対に送らない**
  - `barkclient` は送信先のホストがループバック以外ならエラーにする
- iPhone 側で復号に失敗すると「Decryption Failed」と表示される。これをテストの判定に使える

### 6.4 通知内容の対応（暗号化する前の JSON）

```json
{
  "title": "#channel-name",
  "subtitle": "送信者名",
  "body": "メッセージ本文",
  "group": "slack-relay"
}
```

DM の場合、`title` は `DM` などにする。通知 DB のどのフィールドを使うかは、10 章の調査結果で決める。

### 6.5 watcher（`run`）
- SQLite ドライバ：**`modernc.org/sqlite`**（cgo 不要の純 Go 実装）
- binary plist のパース：`howett.net/plist`
- 依存ライブラリは最小限にし、追加するときは README にその理由を書く
- 通知 DB の読み取り
  - **読み取り専用**で開く（`file:<path>?mode=ro`）。書き込みは一切しない
  - DB がロックされていて読めなかったときは、バックオフしながらリトライする
  - アプリ識別子が Slack のもの（`com.tinyspeck.slackmacgap` を想定。10 章で確認する）のレコードだけを扱う
  - 時刻は Mac の絶対時間（2001-01-01 起点）の可能性が高いので、変換してから比べる
- 起動したときの「最大レコード ID」を記録し、それより新しいものだけを処理する
- 送信に失敗したら数回リトライする。それでも失敗したら、エラーの種類だけをログに残す（**内容はログに残さない**）
- SIGTERM と SIGINT を受けたら、すぐに終了する

### 6.6 秘密情報の管理（`internal/secrets`）
- device_key と AES 鍵は **macOS キーチェーン**に保存する
- 実装は cgo を使わず、`/usr/bin/security` を `exec` で呼び出す
- **秘密情報をコマンドライン引数に載せない**（ps で見えてしまうため）
  - 登録は `security add-generic-password ... -w` を最後に置いて対話入力させるか、標準入力から渡す
  - 読み出しは `find-generic-password -w` の標準出力を受け取る
- 秘密情報を環境変数・設定ファイル・リポジトリに置かない

### 6.7 起動と停止（`toggle` / `internal/procctl`）
- 状態は pid ファイルと `kill -0` で判定する。pid ファイルがあってもプロセスが死んでいれば OFF とみなし、掃除してから起動する
- ON にするときの順序：bark-server → 待ち受けの確認（127.0.0.1 へ接続できるか） → `run`（`Setsid` で切り離す） → `caffeinate -i -w <runのPID>`
- OFF にするとき：`run` と bark-server に SIGTERM を送る。止まらなければ SIGKILL を送る（caffeinate は `run` の終了と一緒に終わる）
- 起動の途中で失敗したら、起動済みのプロセスを片付けてから `⚠️ 起動失敗: <理由の要約>` を出力する
- `raycast/toggle.sh` は Raycast のメタデータ（`@raycast.schemaVersion 1`、`@raycast.title Slack Relay Toggle`、`@raycast.mode silent`、`@raycast.icon 🔔`）を書き、バイナリを呼び出すだけにする。バイナリのパスは `$HOME` を基準にし、ユーザー名をハードコードしない

## 7. macOS 側で必要な設定（docs/setup.md に書く）
- Slack デスクトップアプリの通知設定を「メンションと DM のみ」にする。macOS のシステム設定で Slack の通知を許可する
- フルディスクアクセス：通知 DB を読むために必要。Raycast から起動されたプロセスの場合、権限を付与する先が Raycast なのかバイナリなのかを実機で確認し、書いておく
- 画面ロック中も、Slack の通知が通知センター DB に記録されることを確認する

## 8. 公開リポジトリのためのセキュリティ要件

### 8.1 秘密情報をコミットしない
- P1：device_key、AES 鍵、APNs 関連の鍵、パスワードの類は、一切コミットしない。保存先はキーチェーンだけ
- P2：`.gitignore` で除外するもの：`bin/`、bark-server のバイナリ、`*.db`、`*.sqlite*`、`*.plist`（`testdata/` の合成データは除く）、`.env*`、`config.json`（`config.example.json` は除く）、ログ、`~/.slack-relay` に相当するもの
- P3：**gitleaks** を pre-commit フック（`scripts/install-hooks.sh`）と CI の両方で実行する
- P4：GitHub の Secret scanning と Push protection を有効にする（README の「リポジトリ設定」に書く）
- P5：秘密情報を誤ってコミットした場合は、**履歴から消すだけでは不十分**。鍵を作り直し、デバイスを再登録する。この手順を SECURITY.md に書く

### 8.2 個人情報・業務情報をコミットしない
- P6：ワークスペース名、チャンネル名、ユーザー名、Slack のユーザー ID、Apple ID、電話番号、LAN の IP、ホスト名、`/Users/<実名>` のようなパスをコード・ドキュメント・テスト・コミットメッセージに書かない
- P7：`testdata/` には**合成データだけを置く**。実際の通知 DB、そのダンプ、plist の blob は絶対にコミットしない。テスト用の DB は、テストコードの中でスキーマから生成する
- P8：`docs/investigation.md` にはスキーマや形式だけを書き、実際のレコードの値は書かない
- P9：コミットの作者メールには GitHub の noreply アドレスを使う（README に設定方法を書く）
- P10：Issue や PR にログを貼るときの注意を CONTRIBUTING または README に書く

### 8.3 安全な既定値（設定を間違えても漏れない作り）
- P11：`barkclient` の送信先と bark-server の待ち受けは、ループバック以外を**コードで拒否する**（`register` だけは例外で、LAN IP・時間限定・明示的なコマンド実行が条件）
- P12：平文の `title` / `body` を送るコードパスを作らない（テストで保証する）
- P13：ログはデフォルトで内容を出さない。`internal/redact` を通し、チャンネル名・送信者名・本文・device_key・鍵をマスクする。debug モードでも同じ
- P14：`~/.slack-relay` は `0700`、その中のファイルは `0600` で作る。起動時に権限を確認し、緩ければ警告する
- P15：通知 DB は読み取り専用で開く

### 8.4 シェルスクリプト
- P20：すべてのシェルスクリプトの先頭で `set -euo pipefail` を宣言する
- P21：変数は必ずダブルクォートで囲む。秘密情報をシェルの変数・引数・環境変数で扱わない（秘密情報の処理は Go 側の `setup` に任せる）
- P22：ダウンロードは `curl --fail --proto '=https' --tlsv1.2` で行い、`curl | sh` のような形でダウンロードしたものを直接実行しない

### 8.5 サプライチェーン
- P16：Go の依存は `go.sum` で固定する。CI で `go mod verify` と **govulncheck** を実行する
- P17：bark-server はバージョンと SHA256 を固定して取得する。更新するときは、上流の変更点（特に外向き通信とログ出力）を確認してからバージョンを上げる
- P18：Dependabot（gomod と github-actions）を有効にする。GitHub Actions は**コミット SHA で固定**する
- P19：CI のワークフローは `permissions: contents: read` を最小権限とし、秘密情報を使わない

### 8.6 CI（GitHub Actions、公開リポジトリなので無料）
- `go build` / `go vet` / `go test ./...`（`internal/crypto` は、既知のベクトルと Bark 互換形式のテストを含める）
- `staticcheck`、`gosec`、`govulncheck`、`gitleaks`
- シェルスクリプトに対して **shellcheck** を実行する
- macOS 固有の部分（キーチェーン、通知 DB）は、インターフェースで分けてモックを使ってテストする

### 8.7 ドキュメント
- `SECURITY.md`：脆弱性の報告方法（GitHub Private vulnerability reporting）と、漏洩したときの対応手順
- `docs/threat-model.md`：信頼する相手（Apple APNs と Bark アプリ）、守るもの、残るリスクを書く
  - 残るリスク：APNs 送信用の鍵は上流で公開されているので、デバイストークンを知っている人は偽の通知を送れる（内容を読むことはできない）。初回起動時に公式サーバーへデバイストークンが登録される
- README の冒頭に、「このツールの安全性は設計を秘密にすることには依存せず、鍵とループバック限定の設計に依存している」と書く

## 9. 受け入れテスト

1. Raycast の Hotkey を 1 回押すと ON になり、HUD が表示される。もう 1 回押すと OFF になる
2. ON の間に、別のアカウント（またはテスト用チャンネル）からメンションを受けると、数秒〜十数秒以内に iPhone に「チャンネル・送信者・本文」が表示される
3. OFF の間のメンションは通知されない。ON にする前のメンションも、ON にした後に送られない
4. ON の間に画面をロックし、ディスプレイがスリープしても通知が届く
5. OFF にした後、Mac mini が通常どおりシステムスリープに入る（`pmset -g assertions` で確認する）
6. bark-server が 127.0.0.1 でだけ待ち受けている（`lsof -iTCP -sTCP:LISTEN -P`）
7. 送信パラメータに平文の title や body が含まれていない（ユニットテストで保証する）
8. 鍵をわざと間違えると、iPhone に「Decryption Failed」と表示される
9. ログに機密情報が含まれていない
10. `gitleaks detect` でリポジトリの全履歴を調べ、検出がゼロであること
11. `doctor` がすべての前提条件を OK と判定する

## 10. 未確定事項（実装の最初に調査すること）

1. **macOS のバージョン**：`sw_vers` で確認する
2. **通知センター DB の場所**：第一候補は `~/Library/Group Containers/group.com.apple.usernoted/db2/db`。なければ `getconf DARWIN_USER_DIR` の配下の `com.apple.notificationcenter/db2/db` を確認する
3. **DB のスキーマ**：テーブル構成（`record` と `app` を想定）、時刻とレコード ID のカラム
4. **Slack の bundle id**：`app` テーブルで確認する
5. **Slack 通知の表示形式**：チャンネルのメンション、スレッドのメンション、DM の 3 パターンで、title・subtitle・body のどこに何が入るかを確認し、6.4 節の対応を決める
6. **フルディスクアクセスを付与する先**（7 章を参照）

調査結果は `docs/investigation.md` に**形式だけ**を書き残す（8.2 の P8 を守る）。

## 11. 対象外（作らないもの）

- GUI アプリやメニューバーアプリ
- 自動起動（ログイン時の起動など）。起動は Raycast からだけにする
- Slack API の利用
- bark-server の自作や改造
- 複数ワークスペースを個別に絞り込む機能

## 付録：信頼性の検証結果（2026 年 10 月時点の GitHub のソース）

- **bark-server**（Finb/bark-server @ 5ea5486、MIT ライセンス）：外向きの通信先は APNs の本番環境（`api.push.apple.com`）だけ。APNs 送信用の鍵はリポジトリに含まれている（Bark アプリの公式な鍵）。待ち受けのデフォルトは `0.0.0.0:8080`。アクセスログには URL とリクエストボディが出る。知らないパラメータ（`ciphertext`、`iv`）は、そのまま APNs のペイロードに転送される
- **Bark iOS アプリ**（Finb/Bark @ 1f3da9a、MIT ライセンス）：解析やトラッキング用の SDK はない。復号は Notification Service Extension の中で、端末上の鍵を使って行われる。`iv` パラメータが付いていればそれを優先する。メッセージの履歴は端末内の Realm DB に保存され、iCloud 同期はない。HTTP での接続を許可している（ATS 例外）。Basic 認証への対応は確認できなかった。初回起動時には、公式サーバー（api.day.app）にデバイストークンを登録する。App Store 版は GitHub Actions でビルドされ、アプリ内で Run ID を確認できる
