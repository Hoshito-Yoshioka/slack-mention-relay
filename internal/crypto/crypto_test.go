package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"testing"
)

// Synthetic test-only key. Not used anywhere outside tests.
const testKey = "0123456789abcdefghijklmnopqrstuv"

// TestBarkCompatibleVector checks against a vector produced independently
// with Apple CryptoKit (AES.GCM.seal, ciphertext || tag), which is the same
// layout as CryptoSwift's GCM .combined mode used by the Bark app.
func TestBarkCompatibleVector(t *testing.T) {
	const (
		iv   = "abcdefghijkl"
		pt   = `{"title":"#synthetic","subtitle":"Test User","body":"hello","group":"slack-relay"}`
		want = "vEKaleFSdR2pm84bAOqZZVpDj8MnxNcJZleLBs/3Ji7EQ7YqnYpkEz1CnA1q703CSDoZHuson3zIfSSjNFHaBNWCc5tjQJPVFEUHCvDBhY2jt73AK13k1rpmfzs3KLIXb7U="
	)
	got, err := Encrypt(testKey, iv, []byte(pt))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("ciphertext mismatch\n got: %s\nwant: %s", got, want)
	}
	back, err := Decrypt(testKey, iv, got)
	if err != nil || string(back) != pt {
		t.Fatalf("round trip failed: %v", err)
	}
}

// TestNISTVector pins the raw AES-256-GCM output layout (ciphertext || tag)
// with test case 14 of the GCM specification (all-zero key/IV/plaintext).
func TestNISTVector(t *testing.T) {
	block, _ := aes.NewCipher(make([]byte, 32))
	gcm, _ := cipher.NewGCM(block)
	out := gcm.Seal(nil, make([]byte, 12), make([]byte, 16), nil)
	want := "cea7403d4d606b6e074ec5d3baf39d18" + "d0d1c8a799996bf0265b98b5d48ab919"
	if hex.EncodeToString(out) != want {
		t.Fatalf("got %x", out)
	}
}

func TestGenerateKeyAndIV(t *testing.T) {
	k, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateKey(k); err != nil {
		t.Fatalf("generated key invalid: %v", err)
	}
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		iv, err := GenerateIV()
		if err != nil {
			t.Fatal(err)
		}
		if validateIV(iv) != nil {
			t.Fatalf("generated iv invalid: %q", iv)
		}
		if seen[iv] {
			t.Fatalf("iv repeated after %d draws", i)
		}
		seen[iv] = true
	}
}

func TestRejectsBadInput(t *testing.T) {
	cases := []struct{ key, iv string }{
		{"short", "abcdefghijkl"},
		{testKey + "x", "abcdefghijkl"},
		{"0123456789abcdefghijklmnopqrst!v", "abcdefghijkl"},
		{testKey, "short"},
		{testKey, "abcdefghijk!"},
	}
	for _, c := range cases {
		if _, err := Encrypt(c.key, c.iv, []byte("x")); err == nil {
			t.Errorf("Encrypt(%q, %q) succeeded, want error", c.key, c.iv)
		}
	}
}

func TestWrongKeyFailsToDecrypt(t *testing.T) {
	ct, err := Encrypt(testKey, "abcdefghijkl", []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	other := "vutsrqponmlkjihgfedcba9876543210"
	if _, err := Decrypt(other, "abcdefghijkl", ct); err == nil {
		t.Fatal("decrypt with wrong key succeeded")
	}
}
