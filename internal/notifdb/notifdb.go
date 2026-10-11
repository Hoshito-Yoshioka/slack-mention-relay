// Package notifdb reads the macOS Notification Center database
// (usernoted "db2") strictly read-only.
//
// Schema (macOS 26, see docs/investigation.md):
//
//	app(app_id INTEGER PRIMARY KEY, identifier VARCHAR, badge INTEGER NULL)
//	record(rec_id INTEGER PRIMARY KEY, app_id INTEGER, uuid BLOB, data BLOB,
//	       request_date REAL, request_last_date REAL, delivered_date REAL,
//	       presented Bool, style INTEGER, snooze_fire_date REAL)
//
// record.data is a binary plist. Times are Mac absolute time (seconds since
// 2001-01-01 UTC). The DB runs in WAL mode, so it must not be opened with
// immutable=1 (that would skip the WAL and miss new notifications).
package notifdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"howett.net/plist"
	_ "modernc.org/sqlite" // pure-Go SQLite driver (no cgo)
)

// macEpoch is 2001-01-01T00:00:00Z, the Core Foundation absolute time origin.
var macEpoch = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)

// MacAbsToTime converts Mac absolute time to time.Time.
func MacAbsToTime(sec float64) time.Time {
	return macEpoch.Add(time.Duration(sec * float64(time.Second)))
}

// TimeToMacAbs converts time.Time to Mac absolute time.
func TimeToMacAbs(t time.Time) float64 {
	return t.Sub(macEpoch).Seconds()
}

// Notification is one decoded record. Its text fields are content and must
// never be logged.
type Notification struct {
	RecID     int64
	BundleID  string
	Delivered time.Time
	Fields
}

// Fields are the user-visible parts of a notification request.
type Fields struct {
	Title    string
	Subtitle string
	Body     string
	ThreadID string
}

// ErrBusy is returned when the DB is locked; callers should back off.
var ErrBusy = errors.New("notifdb: database busy")

// DB is a read-only handle on the notification database.
type DB struct {
	db        *sql.DB
	bundleIDs []string
}

// DSN builds a read-only SQLite URI for path.
func DSN(path string) string {
	u := url.URL{Scheme: "file", Path: path}
	q := url.Values{}
	q.Set("mode", "ro")
	q.Add("_pragma", "busy_timeout(2000)")
	q.Add("_pragma", "query_only(1)")
	return u.String() + "?" + q.Encode()
}

// Open opens path read-only. bundleIDs selects which apps' records are
// returned (compared case-insensitively).
func Open(path string, bundleIDs []string) (*DB, error) {
	db, err := sql.Open("sqlite", DSN(path))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	ids := make([]string, len(bundleIDs))
	for i, b := range bundleIDs {
		ids[i] = strings.ToLower(b)
	}
	return &DB{db: db, bundleIDs: ids}, nil
}

func (d *DB) Close() error { return d.db.Close() }

// Ping verifies the DB can be read (detects missing Full Disk Access).
func (d *DB) Ping(ctx context.Context) error {
	var n int
	return wrap(d.db.QueryRowContext(ctx, "SELECT count(*) FROM app").Scan(&n))
}

// HasApp reports whether any configured bundle id appears in the app table.
func (d *DB) HasApp(ctx context.Context) (bool, error) {
	q, args := d.inClause()
	var n int
	// #nosec G202 -- only "?" placeholders are concatenated; values are bound
	err := d.db.QueryRowContext(ctx, "SELECT count(*) FROM app WHERE lower(identifier) IN ("+q+")", args...).Scan(&n)
	return n > 0, wrap(err)
}

// MaxRecID returns the largest rec_id currently in the record table.
func (d *DB) MaxRecID(ctx context.Context) (int64, error) {
	var id sql.NullInt64
	err := d.db.QueryRowContext(ctx, "SELECT max(rec_id) FROM record").Scan(&id)
	return id.Int64, wrap(err)
}

// Since returns records of the configured apps with rec_id > after, ordered
// by rec_id. Records whose data cannot be decoded are returned with empty
// Fields so that the caller still advances past them.
func (d *DB) Since(ctx context.Context, after int64) ([]Notification, error) {
	q, args := d.inClause()
	// #nosec G202 -- only "?" placeholders are concatenated; values are bound
	rows, err := d.db.QueryContext(ctx, `
		SELECT r.rec_id, a.identifier, r.data, coalesce(r.delivered_date, r.request_date, 0)
		FROM record r JOIN app a ON a.app_id = r.app_id
		WHERE r.rec_id > ? AND lower(a.identifier) IN (`+q+`)
		ORDER BY r.rec_id`, append([]any{after}, args...)...)
	if err != nil {
		return nil, wrap(err)
	}
	defer rows.Close()
	var out []Notification
	for rows.Next() {
		var (
			n    Notification
			data []byte
			ts   float64
		)
		if err := rows.Scan(&n.RecID, &n.BundleID, &data, &ts); err != nil {
			return nil, wrap(err)
		}
		n.Delivered = MacAbsToTime(ts)
		n.Fields, _ = ParseData(data)
		out = append(out, n)
	}
	return out, wrap(rows.Err())
}

func (d *DB) inClause() (string, []any) {
	ph := make([]string, len(d.bundleIDs))
	args := make([]any, len(d.bundleIDs))
	for i, b := range d.bundleIDs {
		ph[i] = "?"
		args[i] = b
	}
	return strings.Join(ph, ","), args
}

func wrap(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "SQLITE_BUSY") || strings.Contains(msg, "database is locked") {
		return ErrBusy
	}
	return fmt.Errorf("notifdb: %w", err)
}

// ParseData decodes a record.data plist. The request dictionary is stored
// under "req" with abbreviated keys (titl, subt, body, thre).
func ParseData(data []byte) (Fields, error) {
	var top map[string]any
	if _, err := plist.Unmarshal(data, &top); err != nil {
		return Fields{}, fmt.Errorf("notifdb: plist: %w", err)
	}
	req, _ := top["req"].(map[string]any)
	if req == nil {
		return Fields{}, errors.New("notifdb: plist has no req dictionary")
	}
	str := func(k string) string { s, _ := req[k].(string); return s }
	return Fields{
		Title:    str("titl"),
		Subtitle: str("subt"),
		Body:     str("body"),
		ThreadID: str("thre"),
	}, nil
}
