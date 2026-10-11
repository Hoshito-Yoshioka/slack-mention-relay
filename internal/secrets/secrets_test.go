package secrets

import (
	"context"
	"os"
	"runtime"
	"strings"
	"testing"
)

const secretValue = "SyntheticSecretValue0123456789ab"

// fakeSecurity records every invocation and emulates the keychain.
type fakeSecurity struct {
	argvs [][]string
	items map[string]string
}

func (f *fakeSecurity) run(_ context.Context, stdin []byte, args ...string) ([]byte, int, error) {
	f.argvs = append(f.argvs, args)
	switch args[0] {
	case "-i":
		fields := strings.Fields(string(stdin))
		// add-generic-password -U -s S -a A -w V
		f.items[fields[5]] = fields[7]
		return nil, 0, nil
	case "find-generic-password":
		v, ok := f.items[args[4]]
		if !ok {
			return nil, exitNotFound, nil
		}
		return []byte(v + "\n"), 0, nil
	case "delete-generic-password":
		if _, ok := f.items[args[4]]; !ok {
			return nil, exitNotFound, nil
		}
		delete(f.items, args[4])
		return nil, 0, nil
	}
	return nil, 1, nil
}

func TestSecretsNeverInArgv(t *testing.T) {
	f := &fakeSecurity{items: map[string]string{}}
	k := &Keychain{Service: Service, Run: f.run}
	ctx := context.Background()

	if err := k.Set(ctx, AccountAESKey, secretValue); err != nil {
		t.Fatal(err)
	}
	got, err := k.Get(ctx, AccountAESKey)
	if err != nil || got != secretValue {
		t.Fatalf("Get = %q, %v", got, err)
	}
	for _, argv := range f.argvs {
		for _, a := range argv {
			if strings.Contains(a, secretValue) {
				t.Fatalf("secret passed in argv: %v", argv)
			}
		}
	}
	if err := k.Delete(ctx, AccountAESKey); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Get(ctx, AccountAESKey); err != ErrNotFound {
		t.Fatalf("after delete: %v", err)
	}
}

func TestRejectsUnsafeValues(t *testing.T) {
	k := &Keychain{Service: Service, Run: (&fakeSecurity{items: map[string]string{}}).run}
	for _, v := range []string{"", "has space", "quote\"", "new\nline", "semi;colon"} {
		if err := k.Set(context.Background(), AccountDeviceKey, v); err == nil {
			t.Errorf("value %q accepted", v)
		}
	}
}

// TestRealKeychain exercises /usr/bin/security with a throwaway service name.
// Opt-in: SMR_KEYCHAIN_TEST=1 go test ./internal/secrets/
func TestRealKeychain(t *testing.T) {
	if runtime.GOOS != "darwin" || os.Getenv("SMR_KEYCHAIN_TEST") != "1" {
		t.Skip("set SMR_KEYCHAIN_TEST=1 on macOS to run")
	}
	k := NewKeychain()
	k.Service = "slack-mention-relay-selftest"
	ctx := context.Background()
	_ = k.Delete(ctx, "probe")
	if err := k.Set(ctx, "probe", secretValue); err != nil {
		t.Fatal(err)
	}
	defer k.Delete(ctx, "probe")
	if got, err := k.Get(ctx, "probe"); err != nil || got != secretValue {
		t.Fatalf("Get = %q, %v", got, err)
	}
}
