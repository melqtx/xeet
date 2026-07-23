//go:build darwin

package api

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"testing"
	"time"
)

// encryptLikeChrome mirrors Chromium's macOS cookie encryption so we can verify
// decryptChromeValue round-trips. domainHash, when non-nil, is prepended to the
// plaintext the way recent Chrome does.
func encryptLikeChrome(t *testing.T, key []byte, plaintext string, domainHash []byte) []byte {
	t.Helper()
	data := append(append([]byte{}, domainHash...), []byte(plaintext)...)

	// PKCS7 pad.
	pad := aes.BlockSize - len(data)%aes.BlockSize
	data = append(data, bytes.Repeat([]byte{byte(pad)}, pad)...)

	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	iv := bytes.Repeat([]byte{' '}, aes.BlockSize)
	out := make([]byte, len(data))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, data)
	return append([]byte("v10"), out...)
}

func TestDecryptChromeValueRoundTrip(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 16) // any 16-byte key works for the round trip
	want := "1234567890abcdef1234567890abcdef12345678"

	enc := encryptLikeChrome(t, key, want, nil)
	got, err := decryptChromeValue(enc, key)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if got != want {
		t.Fatalf("round trip mismatch: got %q want %q", got, want)
	}
}

func TestDecryptChromeValueStripsDomainHash(t *testing.T) {
	key := bytes.Repeat([]byte{0x17}, 16)
	want := "ct0value_hex_here"
	// 32 non-printable bytes simulating the SHA-256 domain-hash prefix.
	prefix := bytes.Repeat([]byte{0x00, 0x01, 0x02, 0x03}, 8)

	enc := encryptLikeChrome(t, key, want, prefix)
	got, err := decryptChromeValue(enc, key)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if got != want {
		t.Fatalf("prefix not stripped: got %q want %q", got, want)
	}
}

func TestPkcs7Unpad(t *testing.T) {
	if _, err := pkcs7Unpad([]byte{}); err == nil {
		t.Error("empty input should error")
	}
	if _, err := pkcs7Unpad([]byte{0x05, 0x05}); err == nil {
		t.Error("pad length exceeding data should error")
	}
	out, err := pkcs7Unpad([]byte{'h', 'i', 0x02, 0x02})
	if err != nil || string(out) != "hi" {
		t.Errorf("got %q, %v", out, err)
	}
}

func TestChromeCookieTime(t *testing.T) {
	const unixEpochInChromeMicroseconds = int64(11_644_473_600_000_000)
	if got := chromeCookieTime(unixEpochInChromeMicroseconds); !got.Equal(time.Unix(0, 0).UTC()) {
		t.Fatalf("Chrome epoch converted to %v", got)
	}
	want := time.Date(2026, 7, 23, 12, 34, 56, 789_000_000, time.UTC)
	chromeValue := unixEpochInChromeMicroseconds + want.UnixMicro()
	if got := chromeCookieTime(chromeValue); !got.Equal(want) {
		t.Fatalf("converted=%v want=%v", got, want)
	}
	if got := chromeCookieTime(0); !got.IsZero() {
		t.Fatalf("zero Chrome time converted to %v", got)
	}
}
