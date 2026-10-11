# セットアップ手順

以下、`relay` は `~/.slack-relay/bin/slack-mention-relay` を指します。

## 0. iPhone に Bark アプリを入れる

1. 次のリンクから App Store を開き、インストールする（無料）
   - <https://apps.apple.com/app/bark-custom-notifications/id1403753865>
   - アプリ名：**Bark - Custom Notifications**（App Store の ID：1403753865）
   - デベロッパ名：**丰 黄**（日本の App Store での表記）
   - 検索すると似た名前のアプリが複数出てくるので、上のリンクから入れること。リンクは公式リポジトリの README に載っているものと同じ
   - ソース：[Finb/Bark](https://github.com/Finb/Bark)。App Store 版は GitHub Actions でビルドされ、アプリ内で Run ID を確認できる
2. アプリを起動し、**通知を許可する**（許可しないと何も届かない）
3. 初回起動時、アプリは公式サーバー（api.day.app）にデバイストークンを登録する。これは仕様で、残るリスクとして [threat-model.md](threat-model.md) に書いてある。公式サーバーは「3. 初回のデバイス登録」の手順 4 で削除する
4. この時点では、アプリ内の設定は何も変えなくてよい

## 1. ビルドと bark-server の取得

```bash
go build -o ~/.slack-relay/bin/slack-mention-relay ./cmd/slack-mention-relay && chmod 700 ~/.slack-relay/bin/slack-mention-relay
scripts/install-bark-server.sh
```

`install-bark-server.sh` は、固定したバージョンの bark-server を GitHub Releases から取得します。SHA256 が一致しなければ中止します。

## 2. macOS の設定

1. **Slack の通知**
   - Slack デスクトップアプリ → 設定 → 通知 で、「メンションと DM のみ」（ダイレクトメッセージ、メンション、キーワード）にする
   - システム設定 → 通知 → Slack で、通知を許可する
2. **フルディスクアクセス**
   - 通知センター DB（`~/Library/Group Containers/group.com.apple.usernoted/db2/db`）を読むために必要です
   - Raycast から ON にした場合、権限は **Raycast** に付与します（システム設定 → プライバシーとセキュリティ → フルディスクアクセス）
   - ターミナルで `relay doctor` や `relay run` を実行する場合は、そのターミナルアプリにも付与が必要です
   - TODO（実機確認）：Raycast への付与だけで、`toggle` から切り離して起動した `run` が DB を読めることを確認する
3. **画面ロック中の記録**
   - TODO（実機確認）：画面ロック中も、Slack の通知が通知センター DB に記録されることを確認する

## 3. 初回のデバイス登録

iPhone の Bark アプリが bark-server に接続するのは、デバイスを登録するときだけです。

1. relay を OFF にした状態で、次を実行する
   ```bash
   relay register
   ```
   - Mac の LAN IP（プライベート IP のみ。`0.0.0.0` は使わない）で bark-server を起動します
   - **5 分たつと自動で停止します。** 登録を検知した場合は、その時点で停止します
   - Bark アプリは Basic 認証に対応していないため、「LAN 限定・時間限定」で守ります
2. iPhone を同じ Wi-Fi に接続し、Bark アプリ → サーバー → 追加 で、表示された `http://<IP>:<port>` を入力する
3. 次を実行する
   ```bash
   relay setup
   ```
   - AES 鍵が生成され、**一度だけ**表示されます。Bark アプリ → 設定 → 暗号化設定 で、AES256 / GCM / noPadding とこの鍵を入力します
   - 続けて、Bark アプリに表示された device_key を入力します（入力内容は画面に表示されません）
   - 鍵と device_key はキーチェーンにだけ保存されます
4. **Bark アプリから公式サーバー（api.day.app）を削除する**
   - サーバーが 0 件になると公式サーバーが自動で再追加される仕様のため、必ず「自前サーバーを追加した後に」削除します
5. 以後、bark-server は 127.0.0.1 でだけ起動します
6. iPhone の機種変更やアプリの再インストールをしたときは、この手順をやり直します

## 4. 動作確認

```bash
relay test-notify              # 「暗号化テスト通知です…」が表示されれば成功
relay test-notify --wrong-key  # 「Decryption Failed」が表示されれば正常
relay doctor                   # すべて OK になること
```

## 5. Raycast の設定

1. Raycast → Settings → Extensions → Script Commands → Add Directories で、このリポジトリの `raycast/` を追加する
2. `Slack Relay Toggle` に Hotkey を割り当てる
3. Hotkey を押すと `🔔 Slack relay ON` / `🔕 Slack relay OFF` が HUD に表示される

起動に失敗したときは、`⚠️ 起動失敗: <理由>` が表示されます。詳細は `relay doctor` と `~/.slack-relay/relay.log`（内容はマスク済み）で確認してください。

## 6. 受け入れテストの確認方法

| 確認すること | 方法 |
|---|---|
| システムスリープだけを防いでいる | ON の間に `pmset -g assertions` で、`caffeinate` の PreventUserIdleSystemSleep だけが出ていること。OFF にした後は消えていること |
| ループバックでだけ待ち受けている | `lsof -iTCP -sTCP:LISTEN -P \| grep bark` で `127.0.0.1:<port>` だけであること（`doctor` でも確認できる） |
| 全履歴に秘密情報がない | `gitleaks git --redact` で検出がゼロであること |
