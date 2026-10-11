package main

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestRegisteredRe(t *testing.T) {
	// Synthetic lines in bark-server v2.3.7's access log format.
	yes := []string{
		`2026-01-01 00:00:00     INFO    192.0.2.10 -> [200] POST 1.2ms /register => /register {"device_token":"synthetic"}`,
		`2026-01-01 00:00:00     INFO    192.0.2.10 -> [200] GET 850µs /register => /register?devicetoken=synthetic `,
	}
	no := []string{
		`2026-01-01 00:00:00     INFO    192.0.2.10 -> [400] POST 1.2ms /register => /register {}`,
		`2026-01-01 00:00:00     INFO    192.0.2.10 -> [200] GET 1.2ms /register/:device_key => /register/abc `,
		`2026-01-01 00:00:00     INFO    192.0.2.10 -> [200] GET 1.2ms /ping => /ping `,
		`2026-01-01 00:00:00     INFO    192.0.2.10 -> [200] POST 1.2ms /:device_key => /register {}`,
	}
	for _, l := range yes {
		if !registeredRe.MatchString(l) {
			t.Errorf("should match: %s", l)
		}
	}
	for _, l := range no {
		if registeredRe.MatchString(l) {
			t.Errorf("should not match: %s", l)
		}
	}
}

func TestSummarizeIsOneShortLine(t *testing.T) {
	s := summarize(errors.New("line one\nline two " + strings.Repeat("x", 200)))
	if strings.Contains(s, "\n") || utf8.RuneCountInString(s) > 81 {
		t.Fatalf("got %q", s)
	}
}
