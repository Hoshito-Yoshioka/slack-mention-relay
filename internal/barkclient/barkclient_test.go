package barkclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	relaycrypto "slack-mention-relay/internal/crypto"
)

const (
	testKey       = "0123456789abcdefghijklmnopqrstuv"
	testDeviceKey = "SyntheticDeviceKey01"
)

var testMsg = Message{
	Title:    "#synthetic-channel",
	Subtitle: "Synthetic Sender",
	Body:     "synthetic body text",
	Group:    "slack-relay",
}

type captured struct {
	path string
	raw  string
	form url.Values
}

func newServer(t *testing.T, reply string) (*httptest.Server, *captured) {
	t.Helper()
	c := &captured{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		c.path = r.URL.Path
		c.raw = r.URL.RawQuery + "\n" + string(b)
		c.form, _ = url.ParseQuery(string(b))
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	return srv, c
}

// P12 / acceptance test 7: only ciphertext and iv are sent; no plaintext.
func TestSendsOnlyCiphertextAndIV(t *testing.T) {
	srv, got := newServer(t, `{"code":200,"message":"success"}`)
	c, err := New(strings.TrimPrefix(srv.URL, "http://"), testDeviceKey, testKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Send(context.Background(), testMsg); err != nil {
		t.Fatal(err)
	}

	if got.path != "/"+testDeviceKey {
		t.Fatalf("path = %q", got.path)
	}
	if len(got.form) != 2 || got.form.Get("ciphertext") == "" || got.form.Get("iv") == "" {
		t.Fatalf("form keys = %v, want exactly ciphertext and iv", keys(got.form))
	}
	for _, s := range []string{testMsg.Title, testMsg.Subtitle, testMsg.Body, "synthetic", "title=", "body=", "subtitle="} {
		if strings.Contains(got.raw, s) || strings.Contains(got.raw, url.QueryEscape(s)) {
			t.Errorf("request contains plaintext %q", s)
		}
	}

	pt, err := relaycrypto.Decrypt(testKey, got.form.Get("iv"), got.form.Get("ciphertext"))
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	var back Message
	if err := json.Unmarshal(pt, &back); err != nil || back != testMsg {
		t.Fatalf("decrypted = %+v (%v)", back, err)
	}
}

func TestFreshIVPerMessage(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		f, err := EncryptForm(testKey, testMsg, relaycrypto.GenerateIV)
		if err != nil {
			t.Fatal(err)
		}
		if seen[f.Get("iv")] {
			t.Fatal("iv reused")
		}
		seen[f.Get("iv")] = true
	}
}

func TestRejectsNonLoopback(t *testing.T) {
	for _, addr := range []string{
		"192.168.1.10:8080", "10.0.0.1:80", "0.0.0.0:8080", "[::]:8080",
		"localhost:8080", "example.com:80", "api.day.app:443", "127.0.0.1", "127.0.0.1:0",
	} {
		if _, err := New(addr, testDeviceKey, testKey); err == nil {
			t.Errorf("New(%q) succeeded, want loopback error", addr)
		}
	}
	for _, addr := range []string{"127.0.0.1:18787", "[::1]:18787"} {
		if _, err := New(addr, testDeviceKey, testKey); err != nil {
			t.Errorf("New(%q) = %v", addr, err)
		}
	}
}

func TestRejectsBadDeviceKey(t *testing.T) {
	for _, k := range []string{"", "short", "has/slash/in/it", "has space in it", "../../etc/passwd"} {
		if _, err := New("127.0.0.1:18787", k, testKey); err == nil {
			t.Errorf("device key %q accepted", k)
		}
	}
}

func TestErrorsDoNotLeak(t *testing.T) {
	srv, _ := newServer(t, `{"code":400,"message":"failed"}`)
	c, _ := New(strings.TrimPrefix(srv.URL, "http://"), testDeviceKey, testKey)
	err := c.Send(context.Background(), testMsg)
	if err == nil || err.(*SendError).Kind() != "bark_code_400" {
		t.Fatalf("err = %v", err)
	}

	srv.Close()
	err = c.Send(context.Background(), testMsg)
	if err == nil {
		t.Fatal("want error from closed server")
	}
	if strings.Contains(err.Error(), testDeviceKey) || strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("error leaks destination: %v", err)
	}
}

func keys(v url.Values) []string {
	var k []string
	for key := range v {
		k = append(k, key)
	}
	return k
}
