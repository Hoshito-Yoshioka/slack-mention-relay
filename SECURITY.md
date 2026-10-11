# セキュリティポリシー

## 脆弱性の報告

脆弱性を見つけた場合は、公開の Issue ではなく **GitHub の Private vulnerability reporting**（リポジトリの Security タブ → "Report a vulnerability"）で報告してください。

報告には、再現手順と影響範囲を書いてください。実際の通知内容、鍵、device_key、ワークスペースの情報は含めないでください。

## 秘密情報が漏洩した・誤ってコミットした場合

**履歴から消すだけでは不十分です。** 公開リポジトリに一度でも push された値は、すでに複製されていると考えてください。必ず鍵を作り直してください。

### AES 鍵が漏洩した場合

1. 鍵を作り直す
   ```bash
   ~/.slack-relay/bin/slack-mention-relay setup --rotate-key
   ```
2. 表示された新しい鍵を、iPhone の Bark アプリの暗号化設定に入力する
3. `slack-mention-relay test-notify` で復号できることを確認する
4. `test-notify --wrong-key` で「Decryption Failed」と表示されることを確認する

### device_key が漏洩した場合

device_key を知っている人は、（bark-server に到達できれば）偽の通知を送れます。内容を読むことはできません。

1. Bark アプリで自前サーバーを削除し、[docs/setup.md](docs/setup.md) の「初回のデバイス登録」をやり直して、新しい device_key を発行する
2. `slack-mention-relay setup` で新しい device_key を登録する
3. 古い登録は `~/.slack-relay/bark-data` を削除すると消えます（relay を OFF にしてから行う）

### 誤ってコミットした場合の履歴の掃除

鍵を作り直したうえで、次を行います。

1. `git filter-repo` などで該当するファイルや値を履歴から削除し、force push する
2. GitHub Support に、キャッシュされたビューの削除を依頼する（必要な場合）
3. `gitleaks git` で全履歴を再スキャンし、検出がゼロであることを確認する

## 設計上の前提

脅威モデルと、残るリスクは [docs/threat-model.md](docs/threat-model.md) を参照してください。
