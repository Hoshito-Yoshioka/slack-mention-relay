# セキュリティポリシー

## サポート対象

セキュリティ修正は、`main` ブランチの最新版に対してだけ行います。

## 脆弱性の報告

脆弱性を見つけた場合は、**公開の Issue には書かず**、GitHub の **Private vulnerability reporting** で報告してください（リポジトリの Security タブ → "Report a vulnerability"）。

報告には、次のことを書いてください。

- 影響の内容（何が漏れるのか、誰が何をできるようになるのか）
- 再現手順と、再現した環境（macOS、Slack、bark-server、Bark アプリのバージョン）

実際の通知内容、暗号鍵、device_key、ワークスペースの情報は含めないでください。

本プロジェクトは個人が運営しているため、対応はベストエフォートです。修正を公開するまでは、報告の内容を公開しないようお願いします。

### 対象範囲

| 対象                             | 例                                                                                                                                                                            |
| -------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 対象                             | このリポジトリのコード、スクリプト、CI 設定。たとえば、暗号化を回避できる、ログに内容が漏れる、ループバック以外に公開されてしまう、など                                       |
| 対象外（上流に報告してください） | bark-server 本体の脆弱性は [Finb/bark-server](https://github.com/Finb/bark-server) に、Bark iOS アプリの脆弱性は [Finb/Bark](https://github.com/Finb/Bark) に報告してください |
| 対象外（既知の仕様）             | [docs/threat-model.md](docs/threat-model.md) の「残るリスク」に書いてあるもの                                                                                                 |

## 利用者向け：暗号鍵や device_key が漏れた場合

漏れた値は、すでに第三者に知られていると考えてください。消すだけでなく、必ず作り直してください。

### 暗号鍵（AES 鍵）が漏れた場合

暗号鍵を知っている人は、APNs 上で暗号文を入手できれば、通知の中身を読めます。

1. 鍵を作り直す
   ```bash
   ~/.slack-relay/bin/slack-mention-relay setup --rotate-key
   ```
2. 表示された新しい鍵を、iPhone の Bark アプリの暗号化設定に入力する
3. `slack-mention-relay test-notify` で、通知が読めることを確認する
4. `slack-mention-relay test-notify --wrong-key` で、「Decryption Failed」と表示されることを確認する

### device_key が漏れた場合

device_key を知っている人は、bark-server に到達できれば偽の通知を送れます。中身を読むことはできません。

1. relay を OFF にし、`~/.slack-relay/bark-data` を削除して、古い登録を消す
2. Bark アプリから自分の Mac のサーバーを削除し、[docs/setup.md](docs/setup.md) の「初回のデバイス登録」をやり直す
3. `slack-mention-relay setup` で、新しい device_key を登録する

## コントリビューター向け：秘密情報を誤ってコミットした場合

**履歴から消すだけでは不十分です。** 公開リポジトリに一度でも push された値は、すでに複製されていると考えてください。

1. まず、上の手順で鍵や device_key を作り直す
2. `git filter-repo` などで、該当するファイルや値を履歴から削除し、force push する
3. 必要に応じて、GitHub Support にキャッシュされたビューの削除を依頼する
4. `gitleaks git --redact` で全履歴を再スキャンし、検出がゼロであることを確認する
