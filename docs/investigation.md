# 通知センター DB の調査メモ

このファイルには、スキーマと形式だけを書きます。実際のレコードの値（チャンネル名、ユーザー名、本文など）は書きません（REQUIREMENTS 8.2 P8）。

## 調査環境

- macOS 26.6（arm64）
- Slack デスクトップ 4.48

## 1. DB の場所

| 候補 | 結果 |
|---|---|
| `~/Library/Group Containers/group.com.apple.usernoted/db2/db` | **ある**（macOS 15 以降の場所。TCC で保護されている） |
| `$(getconf DARWIN_USER_DIR)/com.apple.notificationcenter/db2/db` | ない（macOS 14 以前の場所） |

- ジャーナルモードは **WAL**（`db`、`db-wal`、`db-shm`）
  - 読み取り専用で開くときは `mode=ro` を使う
  - `immutable=1` は使わない。WAL を読み飛ばすため、新しい通知が見えなくなる
- 同じディレクトリに `db.corrupt*` が残っていることがある（過去の破損からの復旧の跡）。本ツールは扱わない

## 2. スキーマ

```sql
CREATE TABLE dbinfo (key VARCHAR, value VARCHAR);
CREATE TABLE app (app_id INTEGER PRIMARY KEY, identifier VARCHAR, badge INTEGER NULL);
CREATE TABLE record (rec_id INTEGER PRIMARY KEY, app_id INTEGER, uuid BLOB, data BLOB,
                     request_date REAL, request_last_date REAL, delivered_date REAL,
                     presented Bool, style INTEGER, snooze_fire_date REAL);
CREATE TABLE requests  (app_id INTEGER PRIMARY KEY, list BLOB);
CREATE TABLE delivered (app_id INTEGER PRIMARY KEY, list BLOB);
CREATE TABLE displayed (app_id INTEGER PRIMARY KEY, list BLOB);
CREATE TABLE snoozed   (app_id INTEGER PRIMARY KEY, list BLOB);
CREATE TABLE categories (app_id INTEGER PRIMARY KEY, categories BLOB);
-- app を削除すると、関連する行を消すトリガーがある
```

- レコード ID：`record.rec_id`（INTEGER PRIMARY KEY、AUTOINCREMENT なし）
- 時刻：`record.delivered_date`（Mac の絶対時間。2001-01-01 UTC からの秒数、REAL）。NULL の場合は `request_date` を使う
- 本文など：`record.data`（binary plist）

## 3. Slack の bundle id

- `/Applications/Slack.app` の `CFBundleIdentifier` は `com.tinyspeck.slackmacgap`
- `app.identifier` に `com.tinyspeck.slackmacgap` があることを確認済み（`doctor` で確認。大文字・小文字を区別しない比較）

## 4. `record.data`（binary plist）の形式

公開情報をもとにした想定です（TODO：実機で確認する）。

```
{
  app:  <bundle id>
  date: <REAL>
  req: {
    titl: <string>   タイトル
    subt: <string>   サブタイトル
    body: <string>   本文
    thre: <string>   スレッド ID
    iden: <string>   リクエスト ID
    ...
  }
  ...
}
```

## 5. Slack の通知の表示形式

TODO（実機確認）：次の 3 パターンで、`titl` / `subt` / `body` のどれに「チャンネル」「送信者」「本文」が入るかを確認し、[internal/watcher/format.go](../internal/watcher/format.go) の対応を決める。

| パターン | titl | subt | body |
|---|---|---|---|
| チャンネルでのメンション | ? | ? | ? |
| スレッドでのメンション | ? | ? | ? |
| DM | ? | ? | ? |

### 確認の手順

値そのものはこのファイルにも Issue にも書かず、「何が入っていたか」だけを書きます。

1. **準備**
   - Slack デスクトップアプリの通知を「メンションと DM のみ」にし、macOS 側でも Slack の通知を許可する
   - 集中モード（おやすみモード）を切る
   - 別のアカウントの人（同僚など）に協力を頼む。テスト用のチャンネルがあるとよい
2. **通知を 3 件受ける**（この順番で、1 件ずつ）
   1. チャンネルでのメンション：テスト用チャンネルで `@自分 テスト1`
   2. スレッドでのメンション：どこかのメッセージのスレッドで `@自分 テスト2`
   3. DM：自分宛ての DM で `テスト3`
   - Slack のウィンドウを前面に出していると通知が出ないことがあるので、Slack は最小化するか、別のアプリを前面にしておく
   - 3 件とも Mac の画面右上に通知バナーが出たことを確認する
3. **形式を出力する**（値は出力されません）
   ```bash
   ~/.slack-relay/bin/slack-mention-relay inspect -n 3
   ```
   - 「1 件目」が最も新しい通知（= DM）、「3 件目」がチャンネルでのメンション
   - 「通知 DB を読めません」と出た場合は、実行しているターミナルアプリにフルディスクアクセスを付与する
4. **表示を目で見て確認する**
   - 画面右上の時計をクリックして通知センターを開き、3 件の Slack 通知を見る
   - macOS の通知は、1 行目（太字）が `titl`、2 行目（太字、ない場合もある）が `subt`、その下が `body`
   - 各行に何が入っていたかを、上の表に「チャンネル名」「送信者名」「本文」「送信者名: 本文」「ワークスペース名」「空」のような**言葉で**書く
5. **共有するもの**：手順 3 の出力（そのまま貼ってよい）と、手順 4 の表

## 6. bark-server（上流 v2.3.7）

- 起動フラグ：`--addr`（既定は `0.0.0.0:8080`）、`--data`。各フラグは `BARK_SERVER_*` 環境変数でも指定できる。そのため子プロセスに渡す環境変数は最小限に絞っている
- アクセスログ（標準出力）の形式：`<time> INFO <ip> -> [<status>] <method> <latency> <route> => <url> <body>`
  - URL とリクエストボディが出る。通常運用では `/dev/null` に捨てる
  - `register` では、この行をメモリ上で照合して `/register` の成功を検知する。照合した行は保存も表示もしない
- データディレクトリは `0755` で作られる。本ツールは umask を `0077` にしてから bark-server を起動する
- ルート：`/register`、`/:device_key`（push）、`/ping`、`/healthz`、`/info`、MCP 関連
