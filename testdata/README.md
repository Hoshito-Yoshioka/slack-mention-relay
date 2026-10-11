# testdata

合成データだけを置くディレクトリです。実際の通知 DB、そのダンプ、plist の blob は絶対に置かないでください。
通知 DB のテストは、`internal/notifdb/notifdb_test.go` の中でスキーマから DB を生成しています。
