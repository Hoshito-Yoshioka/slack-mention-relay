package notifdb

import (
	"context"
	"fmt"
	"sort"
	"time"

	"howett.net/plist"
)

// RawRecord is an undecoded record, used only by Describe-based inspection.
type RawRecord struct {
	RecID     int64
	Delivered time.Time
	Data      []byte
}

// Latest returns the newest n records of the configured apps, newest first.
func (d *DB) Latest(ctx context.Context, n int) ([]RawRecord, error) {
	q, args := d.inClause()
	// #nosec G202 -- only "?" placeholders are concatenated; values are bound
	rows, err := d.db.QueryContext(ctx, `
		SELECT r.rec_id, r.data, coalesce(r.delivered_date, r.request_date, 0)
		FROM record r JOIN app a ON a.app_id = r.app_id
		WHERE lower(a.identifier) IN (`+q+`)
		ORDER BY r.rec_id DESC LIMIT ?`, append(args, n)...)
	if err != nil {
		return nil, wrap(err)
	}
	defer rows.Close()
	var out []RawRecord
	for rows.Next() {
		var (
			r  RawRecord
			ts float64
		)
		if err := rows.Scan(&r.RecID, &r.Data, &ts); err != nil {
			return nil, wrap(err)
		}
		r.Delivered = MacAbsToTime(ts)
		out = append(out, r)
	}
	return out, wrap(rows.Err())
}

// Describe returns the structure of a record.data plist as lines of
// "<key path>  <type>  <length>". It never includes values, so its output is
// safe to share (REQUIREMENTS 8.2 P8).
func Describe(data []byte) ([]string, error) {
	var top any
	if _, err := plist.Unmarshal(data, &top); err != nil {
		return nil, fmt.Errorf("notifdb: plist: %w", err)
	}
	var lines []string
	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch t := v.(type) {
		case map[string]any:
			keys := make([]string, 0, len(t))
			for k := range t {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				walk(path+"."+k, t[k])
			}
		case []any:
			lines = append(lines, fmt.Sprintf("%-28s array   len=%d", path, len(t)))
		case string:
			lines = append(lines, fmt.Sprintf("%-28s string  len=%d", path, len([]rune(t))))
		case []byte:
			lines = append(lines, fmt.Sprintf("%-28s data    len=%d", path, len(t)))
		case bool:
			lines = append(lines, fmt.Sprintf("%-28s bool", path))
		case time.Time:
			lines = append(lines, fmt.Sprintf("%-28s date", path))
		case uint64, int64, float64:
			lines = append(lines, fmt.Sprintf("%-28s number", path))
		default:
			lines = append(lines, fmt.Sprintf("%-28s %T", path, v))
		}
	}
	walk("", top)
	return lines, nil
}
