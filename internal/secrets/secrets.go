// Package secrets stores the AES key and device_key in the macOS login
// keychain via /usr/bin/security (no cgo).
//
// Secrets never appear in command-line arguments (visible in ps): writes are
// sent as a command line on stdin to `security -i`, reads come back on stdout.
package secrets

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

const (
	Service = "slack-mention-relay"

	AccountAESKey    = "aes_key"
	AccountDeviceKey = "device_key"

	securityBin  = "/usr/bin/security"
	exitNotFound = 44
)

var ErrNotFound = errors.New("secrets: item not found in keychain")

// Store is the interface the rest of the program uses (mocked in tests).
type Store interface {
	Get(ctx context.Context, account string) (string, error)
	Set(ctx context.Context, account, value string) error
	Delete(ctx context.Context, account string) error
}

// Runner executes security(1). args never contain secrets; stdin may.
type Runner func(ctx context.Context, stdin []byte, args ...string) (stdout []byte, exitCode int, err error)

func execRunner(ctx context.Context, stdin []byte, args ...string) ([]byte, int, error) {
	cmd := exec.CommandContext(ctx, securityBin, args...) // #nosec G204 -- fixed binary; args never carry secrets
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return out.Bytes(), ee.ExitCode(), nil
	}
	return out.Bytes(), 0, err
}

// Keychain is the production Store.
type Keychain struct {
	Service string
	Run     Runner
}

func NewKeychain() *Keychain { return &Keychain{Service: Service, Run: execRunner} }

// Only these characters are allowed in stored values and accounts. They need
// no quoting in security's interactive command parser and cover both the
// ASCII AES key and Bark device keys.
var safeValue = regexp.MustCompile(`^[A-Za-z0-9_-]{1,256}$`)

func (k *Keychain) Get(ctx context.Context, account string) (string, error) {
	if !safeValue.MatchString(account) {
		return "", errors.New("secrets: invalid account name")
	}
	out, code, err := k.Run(ctx, nil, "find-generic-password", "-s", k.Service, "-a", account, "-w")
	if err != nil {
		return "", fmt.Errorf("secrets: run security: %w", err)
	}
	if code == exitNotFound {
		return "", ErrNotFound
	}
	if code != 0 {
		return "", fmt.Errorf("secrets: security exited with %d", code)
	}
	return strings.TrimRight(string(out), "\r\n"), nil
}

func (k *Keychain) Set(ctx context.Context, account, value string) error {
	if !safeValue.MatchString(account) {
		return errors.New("secrets: invalid account name")
	}
	if !safeValue.MatchString(value) {
		return errors.New("secrets: value contains unsupported characters")
	}
	line := fmt.Sprintf("add-generic-password -U -s %s -a %s -w %s\n", k.Service, account, value)
	_, code, err := k.Run(ctx, []byte(line), "-i")
	if err != nil {
		return fmt.Errorf("secrets: run security: %w", err)
	}
	if code != 0 {
		return fmt.Errorf("secrets: security exited with %d", code)
	}
	// security -i exits 0 even when a command fails; read back to confirm.
	got, err := k.Get(ctx, account)
	if err != nil {
		return err
	}
	if got != value {
		return errors.New("secrets: keychain write could not be verified")
	}
	return nil
}

func (k *Keychain) Delete(ctx context.Context, account string) error {
	if !safeValue.MatchString(account) {
		return errors.New("secrets: invalid account name")
	}
	_, code, err := k.Run(ctx, nil, "delete-generic-password", "-s", k.Service, "-a", account)
	if err != nil {
		return fmt.Errorf("secrets: run security: %w", err)
	}
	if code == exitNotFound {
		return ErrNotFound
	}
	if code != 0 {
		return fmt.Errorf("secrets: security exited with %d", code)
	}
	return nil
}

// Memory is an in-memory Store for tests.
type Memory map[string]string

func (m Memory) Get(_ context.Context, a string) (string, error) {
	v, ok := m[a]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (m Memory) Set(_ context.Context, a, v string) error { m[a] = v; return nil }

func (m Memory) Delete(_ context.Context, a string) error {
	if _, ok := m[a]; !ok {
		return ErrNotFound
	}
	delete(m, a)
	return nil
}
