// Package watcher polls the notification DB and relays new Slack
// notifications to bark-server.
package watcher

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"slack-mention-relay/internal/barkclient"
	"slack-mention-relay/internal/notifdb"
)

// Source is the subset of notifdb used by the watcher.
type Source interface {
	MaxRecID(ctx context.Context) (int64, error)
	Since(ctx context.Context, after int64) ([]notifdb.Notification, error)
}

// Sender delivers one message.
type Sender interface {
	Send(ctx context.Context, msg barkclient.Message) error
}

// Watcher relays notifications delivered after Start with rec_id above the
// maximum seen at startup. Each rec_id is processed at most once.
type Watcher struct {
	Open     func() (Source, func(), error) // opened per poll; DB file may be replaced
	Sender   Sender
	Format   func(notifdb.Notification) barkclient.Message
	Log      *slog.Logger
	Interval time.Duration
	// Retries is the number of send attempts per notification.
	Retries int
	// Sleep waits for d or until ctx is done (injectable for tests).
	Sleep func(ctx context.Context, d time.Duration) error

	start  time.Time
	lastID int64
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (w *Watcher) defaults() {
	if w.Format == nil {
		w.Format = Format
	}
	if w.Sleep == nil {
		w.Sleep = sleepCtx
	}
	if w.Retries < 1 {
		w.Retries = 3
	}
}

// Init records the start time and the current maximum rec_id. Call it once
// before Run; it doubles as the startup preflight (DB readable).
func (w *Watcher) Init(ctx context.Context, now time.Time) error {
	w.defaults()
	src, closeFn, err := w.Open()
	if err != nil {
		return err
	}
	defer closeFn()
	id, err := src.MaxRecID(ctx)
	if err != nil {
		return err
	}
	w.start, w.lastID = now, id
	return nil
}

// Run polls until ctx is cancelled (SIGTERM/SIGINT). It returns nil on
// cancellation.
func (w *Watcher) Run(ctx context.Context) error {
	w.defaults()
	backoff := time.Duration(0)
	for {
		err := w.Poll(ctx)
		switch {
		case ctx.Err() != nil:
			return nil
		case err != nil:
			// DB locked or briefly unreadable: back off up to 30 s.
			backoff = min(max(backoff*2, w.Interval), 30*time.Second)
			w.Log.Warn("poll failed", "err", err, "delay", backoff.String())
		default:
			backoff = 0
		}
		wait := w.Interval
		if backoff > 0 {
			wait = backoff
		}
		if w.Sleep(ctx, wait) != nil {
			return nil
		}
	}
}

// Poll processes all new notifications once.
func (w *Watcher) Poll(ctx context.Context) error {
	src, closeFn, err := w.Open()
	if err != nil {
		return err
	}
	defer closeFn()
	ns, err := src.Since(ctx, w.lastID)
	if err != nil {
		return err
	}
	for _, n := range ns {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Advance first: a notification is attempted at most once even if
		// sending fails, so a persistent failure can't cause a resend storm.
		w.lastID = n.RecID
		if n.Delivered.Before(w.start) {
			continue
		}
		w.send(ctx, n)
	}
	return nil
}

func (w *Watcher) send(ctx context.Context, n notifdb.Notification) {
	msg := w.Format(n)
	var err error
	for attempt := 1; attempt <= w.Retries; attempt++ {
		if err = w.Sender.Send(ctx, msg); err == nil {
			w.Log.Info("relayed", "rec_id", n.RecID)
			return
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			return
		}
		w.Log.Warn("send failed", "rec_id", n.RecID, "attempt", attempt, "err", err)
		if attempt < w.Retries && w.Sleep(ctx, time.Duration(attempt)*time.Second) != nil {
			return
		}
	}
	w.Log.Error("giving up", "rec_id", n.RecID, "err", err)
}
