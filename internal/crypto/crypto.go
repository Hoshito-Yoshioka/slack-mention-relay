// Package crypto implements the Bark-compatible end-to-end encryption:
// AES-256-GCM (no padding), a 32-character ASCII key, a fresh 12-character
// ASCII IV per message, and base64(ciphertext || 16-byte tag) as output.
//
// The output format matches CryptoSwift's GCM ".combined" mode used by the
// Bark iOS app's Notification Service Extension.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"math/big"
)

const (
	// KeyLen is the AES-256 key length in ASCII characters (= bytes).
	KeyLen = 32
	// IVLen is the GCM nonce length in ASCII characters (= bytes).
	IVLen = 12

	alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
)

var (
	ErrInvalidKey = errors.New("crypto: key must be 32 ASCII alphanumeric characters")
	ErrInvalidIV  = errors.New("crypto: iv must be 12 ASCII alphanumeric characters")
)

// RandomASCII returns n characters drawn uniformly from [A-Za-z0-9]
// using crypto/rand.
func RandomASCII(n int) (string, error) {
	max := big.NewInt(int64(len(alphabet)))
	b := make([]byte, n)
	for i := range b {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b[i] = alphabet[v.Int64()]
	}
	return string(b), nil
}

// GenerateKey returns a new random 32-character ASCII key.
func GenerateKey() (string, error) { return RandomASCII(KeyLen) }

// GenerateIV returns a new random 12-character ASCII IV.
// A new IV must be generated for every message: GCM nonce reuse is fatal.
func GenerateIV() (string, error) { return RandomASCII(IVLen) }

// ValidateKey reports whether key has the format Bark expects.
func ValidateKey(key string) error {
	if !isAlnum(key, KeyLen) {
		return ErrInvalidKey
	}
	return nil
}

func validateIV(iv string) error {
	if !isAlnum(iv, IVLen) {
		return ErrInvalidIV
	}
	return nil
}

func isAlnum(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

func newGCM(key string) (cipher.AEAD, error) {
	if err := ValidateKey(key); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Encrypt seals plaintext and returns base64(ciphertext || tag).
func Encrypt(key, iv string, plaintext []byte) (string, error) {
	if err := validateIV(iv); err != nil {
		return "", err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(gcm.Seal(nil, []byte(iv), plaintext, nil)), nil
}

// Decrypt is the inverse of Encrypt. It exists for tests and self-checks.
func Decrypt(key, iv, ciphertext string) ([]byte, error) {
	if err := validateIV(iv); err != nil {
		return nil, err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, []byte(iv), raw, nil)
}
