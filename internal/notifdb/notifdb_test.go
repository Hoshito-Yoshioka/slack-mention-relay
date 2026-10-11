package notifdb

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"howett.net/plist"
)

// The test DB is generated from the schema; no real data is involved (P7).
const schema = `
CREATE TABLE dbinfo (key VARCHAR, value VARCHAR);
CREATE TABLE app (app_id INTEGER PRIMARY KEY, identifier VARCHAR, badge INTEGER NULL);
CREATE TABLE record (rec_id INTEGER PRIMARY KEY, app_id INTEGER, uuid BLOB, data BLOB, request_date REAL, request_last_date REAL, delivered_date REAL, presented Bool, style INTEGER, snooze_fire_date REAL);
PRAGMA journal_mode=WAL;
`

func synthPlist(t *testing.T, bundle string, f Fields) []byte {
	t.Helper()
	b, err := plist.Marshal(map[string]any{
		"app":  bundle,
		"date": 0.0,
		"req": map[string]any{
			"titl": f.Title, "subt": f.Subtitle, "body": f.Body, "thre": f.ThreadID, "iden": "synthetic-id",
		},
	}, plist.BinaryFormat)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func makeDB(t *testing.T) (string, *sql.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "db")
	w, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	if _, err := w.Exec(schema); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Exec(`INSERT INTO app(app_id, identifier) VALUES (1, 'com.tinyspeck.slackmacgap'), (2, 'com.example.other')`); err != nil {
		t.Fatal(err)
	}
	return path, w
}

func insert(t *testing.T, w *sql.DB, appID int, ts time.Time, data []byte) {
	t.Helper()
	if _, err := w.Exec(`INSERT INTO record(app_id, data, request_date, delivered_date) VALUES (?, ?, ?, ?)`,
		appID, data, TimeToMacAbs(ts), TimeToMacAbs(ts)); err != nil {
		t.Fatal(err)
	}
}

func TestReadOnlyQueries(t *testing.T) {
	path, w := makeDB(t)
	now := time.Now()
	slack := Fields{Title: "#synthetic", Subtitle: "Synthetic Sender", Body: "synthetic body"}
	insert(t, w, 1, now.Add(-time.Hour), synthPlist(t, "com.tinyspeck.slackmacgap", slack))
	insert(t, w, 2, now, synthPlist(t, "com.example.other", Fields{Body: "other"}))

	d, err := Open(path, []string{"COM.tinyspeck.SlackMacGap"})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()

	if err := d.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if ok, err := d.HasApp(ctx); err != nil || !ok {
		t.Fatalf("HasApp = %v, %v", ok, err)
	}
	max, err := d.MaxRecID(ctx)
	if err != nil || max != 2 {
		t.Fatalf("MaxRecID = %d, %v", max, err)
	}

	// New records written while the reader is open (via the WAL) are seen.
	insert(t, w, 1, now.Add(time.Second), synthPlist(t, "com.tinyspeck.slackmacgap", slack))
	insert(t, w, 1, now.Add(2*time.Second), []byte("not a plist"))

	got, err := d.Since(ctx, max)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].RecID != 3 || got[1].RecID != 4 {
		t.Fatalf("Since = %+v", got)
	}
	if got[0].Fields != slack {
		t.Fatalf("fields = %+v", got[0].Fields)
	}
	if d := got[0].Delivered.Sub(now.Add(time.Second)); d > time.Millisecond || d < -time.Millisecond {
		t.Fatalf("time conversion off by %v", d)
	}
	if got[1].Fields != (Fields{}) {
		t.Fatalf("undecodable record should have empty fields: %+v", got[1].Fields)
	}

	// P15: writes through this handle must fail.
	if _, err := d.db.Exec(`DELETE FROM record`); err == nil {
		t.Fatal("write succeeded on read-only handle")
	}
}

func TestOpenDoesNotCreateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing")
	d, err := Open(path, []string{"x"})
	if err == nil {
		err = d.Ping(context.Background())
		d.Close()
	}
	if err == nil {
		t.Fatal("ping on missing DB succeeded")
	}
	if _, statErr := os.Stat(path); statErr == nil {
		t.Fatal("read-only open created the DB file")
	}
}

func TestDSNEscapesSpaces(t *testing.T) {
	dsn := DSN("/Users/someone/Library/Group Containers/x/db")
	want := "file:///Users/someone/Library/Group%20Containers/x/db?"
	if len(dsn) < len(want) || dsn[:len(want)] != want {
		t.Fatalf("dsn = %s", dsn)
	}
}

func TestMacAbsTime(t *testing.T) {
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if !MacAbsToTime(TimeToMacAbs(ts)).Equal(ts) {
		t.Fatal("round trip")
	}
	if MacAbsToTime(0).Unix() != 978307200 {
		t.Fatal("epoch")
	}
}

func TestLatestAndDescribeHideValues(t *testing.T) {
	path, w := makeDB(t)
	now := time.Now()
	secret := Fields{Title: "#synthetic-secret", Subtitle: "Synthetic Secret Sender", Body: "synthetic secret body"}
	insert(t, w, 1, now, synthPlist(t, "com.tinyspeck.slackmacgap", secret))
	insert(t, w, 2, now, synthPlist(t, "com.example.other", Fields{Body: "other"}))
	insert(t, w, 1, now, synthPlist(t, "com.tinyspeck.slackmacgap", secret))

	d, err := Open(path, []string{"com.tinyspeck.slackmacgap"})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	recs, err := d.Latest(context.Background(), 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 || recs[0].RecID != 3 || recs[1].RecID != 1 {
		t.Fatalf("Latest = %+v", recs)
	}
	lines, err := Describe(recs[0].Data)
	if err != nil {
		t.Fatal(err)
	}
	out := strings.Join(lines, "\n")
	for _, s := range []string{"synthetic", "Secret", "slackmacgap", "synthetic-id"} {
		if strings.Contains(out, s) {
			t.Fatalf("Describe leaked %q:\n%s", s, out)
		}
	}
	for _, s := range []string{".req.titl", "len=17", ".req.body", ".app"} {
		if !strings.Contains(out, s) {
			t.Fatalf("Describe missing %q:\n%s", s, out)
		}
	}
}
