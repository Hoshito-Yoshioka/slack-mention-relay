// Package barkclient sends end-to-end encrypted pushes to a local
// bark-server. It refuses any non-loopback destination and has no code path
// that sends title/body as plaintext form parameters: the only parameters
// ever sent are "ciphertext" and "iv".
package barkclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	relaycrypto "slack-mention-relay/internal/crypto"
)

// Message is the plaintext that gets encrypted. Field names follow Bark's
// push parameters.
type Message struct {
	Title    string `json:"title,omitempty"`
	Subtitle string `json:"subtitle,omitempty"`
	Body     string `json:"body"`
	Group    string `json:"group,omitempty"`
}

var deviceKeyRe = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

// ValidateDeviceKey checks the device_key format (it becomes a URL path).
func ValidateDeviceKey(k string) error {
	if !deviceKeyRe.MatchString(k) {
		return errors.New("barkclient: device_key must be 8-128 characters of [A-Za-z0-9_-]")
	}
	return nil
}

// ErrNotLoopback is returned when the destination is not a loopback IP.
var ErrNotLoopback = errors.New("barkclient: destination must be a loopback IP address")

// RequireLoopback checks that addr ("host:port") is a literal loopback IP.
// Host names (even "localhost") are rejected so that DNS can't redirect us.
func RequireLoopback(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return ErrNotLoopback
	}
	if p, err := strconv.Atoi(port); err != nil || p < 1 || p > 65535 {
		return ErrNotLoopback
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return ErrNotLoopback
	}
	return nil
}

// SendError describes a failure without including content, URLs or keys.
type SendError struct{ kind string }

func (e *SendError) Error() string { return "barkclient: send failed: " + e.kind }
func (e *SendError) Kind() string  { return e.kind }

// Client posts encrypted messages to bark-server.
type Client struct {
	endpoint string
	key      string
	http     *http.Client
	newIV    func() (string, error)
}

// New creates a client for bark-server at addr. key is the 32-character AES
// key shared with the Bark app.
func New(addr, deviceKey, key string) (*Client, error) {
	if err := RequireLoopback(addr); err != nil {
		return nil, err
	}
	if err := ValidateDeviceKey(deviceKey); err != nil {
		return nil, err
	}
	if err := relaycrypto.ValidateKey(key); err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "http", Host: addr, Path: "/" + deviceKey}
	return &Client{
		endpoint: u.String(),
		key:      key,
		http: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				Proxy:             nil, // never route through an environment proxy
				DisableKeepAlives: true,
			},
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		newIV: relaycrypto.GenerateIV,
	}, nil
}

// EncryptForm builds the form body for msg: only "ciphertext" and "iv".
func EncryptForm(key string, msg Message, newIV func() (string, error)) (url.Values, error) {
	pt, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	iv, err := newIV()
	if err != nil {
		return nil, err
	}
	ct, err := relaycrypto.Encrypt(key, iv, pt)
	if err != nil {
		return nil, err
	}
	return url.Values{"ciphertext": {ct}, "iv": {iv}}, nil
}

// Send encrypts msg and posts it. Errors never contain the URL or content.
func (c *Client) Send(ctx context.Context, msg Message) error {
	form, err := EncryptForm(c.key, msg, c.newIV)
	if err != nil {
		return &SendError{kind: "encrypt"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return &SendError{kind: "request"}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return &SendError{kind: classify(err)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		return &SendError{kind: fmt.Sprintf("http_status_%d", resp.StatusCode)}
	}
	var r struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return &SendError{kind: "bad_response"}
	}
	if r.Code != http.StatusOK {
		return &SendError{kind: fmt.Sprintf("bark_code_%d", r.Code)}
	}
	return nil
}

func classify(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection_refused"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timeout"
	}
	return "network"
}
