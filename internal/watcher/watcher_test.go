package watcher

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"slack-mention-relay/internal/barkclient"
	"slack-mention-relay/internal/notifdb"
	"slack-mention-relay/internal/redact"
)

type fakeSource struct {
	recs []notifdb.Notification
	err  error
}

func (f *fakeSource) MaxRecID(context.Context) (int64, error) {
	var m int64
	for _, r := range f.recs {
		m = max(m, r.RecID)
	}
	return m, f.err
}

func (f *fakeSource) Since(_ context.Context, after int64) ([]notifdb.Notification, error) {
	if f.err != nil {
		return nil, f.err
	}
	var out []notifdb.Notification
	for _, r := range f.recs {
		if r.RecID > after {
			out = append(out, r)
		}
	}
	return out, nil
}

type fakeSender struct {
	sent  []barkclient.Message
	fails int // fail this many times before succeeding
}

func (f *fakeSender) Send(_ context.Context, m barkclient.Message) error {
	if f.fails > 0 {
		f.fails--
		return errors.New("synthetic failure")
	}
	f.sent = append(f.sent, m)
	return nil
}

func rec(id int64, at time.Time, body string) notifdb.Notification {
	return notifdb.Notification{RecID: id, Delivered: at, Fields: notifdb.Fields{
		Title: "#synthetic", Subtitle: "Synthetic Sender", Body: body,
	}}
}

func newWatcher(src *fakeSource, snd *fakeSender, log *bytes.Buffer) *Watcher {
	return &Watcher{
		Open:     func() (Source, func(), error) { return src, func() {}, nil },
		Sender:   snd,
		Log:      redact.New(log, true),
		Interval: time.Second,
		Sleep:    func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
	}
}

func TestOnlyNewNotificationsOnce(t *testing.T) {
	now := time.Now()
	src := &fakeSource{recs: []notifdb.Notification{rec(10, now.Add(-time.Minute), "old")}}
	snd := &fakeSender{}
	var log bytes.Buffer
	w := newWatcher(src, snd, &log)
	ctx := context.Background()
	if err := w.Init(ctx, now); err != nil {
		t.Fatal(err)
	}

	// rec 11 has a higher id but predates ON (e.g. a late write) -> skipped.
	src.recs = append(src.recs, rec(11, now.Add(-time.Second), "before-on"), rec(12, now.Add(time.Second), "new"))
	if err := w.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	if err := w.Poll(ctx); err != nil { // second poll must not resend
		t.Fatal(err)
	}
	if len(snd.sent) != 1 || snd.sent[0].Body != "new" || snd.sent[0].Group != Group {
		t.Fatalf("sent = %+v", snd.sent)
	}
	for _, s := range []string{"synthetic", "Synthetic Sender", "new"} {
		if strings.Contains(log.String(), s) {
			t.Fatalf("log leaked %q: %s", s, log.String())
		}
	}
}

func TestRetriesThenGivesUp(t *testing.T) {
	now := time.Now()
	src := &fakeSource{}
	snd := &fakeSender{fails: 2}
	var log bytes.Buffer
	w := newWatcher(src, snd, &log)
	_ = w.Init(context.Background(), now)

	src.recs = []notifdb.Notification{rec(1, now.Add(time.Second), "a")}
	_ = w.Poll(context.Background())
	if len(snd.sent) != 1 {
		t.Fatalf("expected success on 3rd attempt, sent=%d", len(snd.sent))
	}

	snd.fails = 10
	src.recs = append(src.recs, rec(2, now.Add(time.Second), "b"))
	_ = w.Poll(context.Background())
	_ = w.Poll(context.Background())
	if len(snd.sent) != 1 || snd.fails != 7 {
		t.Fatalf("expected 3 attempts then give up without resend; sent=%d fails=%d", len(snd.sent), snd.fails)
	}
	if !strings.Contains(log.String(), "giving up") {
		t.Fatal("missing give-up log")
	}
}

func TestRunStopsOnCancel(t *testing.T) {
	src := &fakeSource{}
	var log bytes.Buffer
	w := newWatcher(src, &fakeSender{}, &log)
	w.Sleep = sleepCtx
	w.Interval = 10 * time.Millisecond
	_ = w.Init(context.Background(), time.Now())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- w.Run(ctx) }()
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not stop")
	}
}

func TestRunBacksOffOnBusy(t *testing.T) {
	src := &fakeSource{}
	var log bytes.Buffer
	w := newWatcher(src, &fakeSender{}, &log)
	_ = w.Init(context.Background(), time.Now())
	src.err = notifdb.ErrBusy
	var waits []time.Duration
	ctx, cancel := context.WithCancel(context.Background())
	w.Sleep = func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		if len(waits) == 6 {
			cancel()
			return context.Canceled
		}
		return nil
	}
	_ = w.Run(ctx)
	want := []time.Duration{1, 2, 4, 8, 16, 30}
	for i, d := range want {
		if waits[i] != d*time.Second {
			t.Fatalf("waits = %v", waits)
		}
	}
}

func TestFormat(t *testing.T) {
	m := Format(notifdb.Notification{})
	if m.Title != "Slack" || m.Body == "" || m.Group != Group {
		t.Fatalf("empty fields: %+v", m)
	}
	long := strings.Repeat("あ", 5000)
	m = Format(notifdb.Notification{Fields: notifdb.Fields{Title: "#c", Body: long}})
	b, _ := json.Marshal(m)
	if len(b) > maxPlaintext || !strings.HasSuffix(m.Body, "…") {
		t.Fatalf("not truncated: %d bytes", len(b))
	}
}
