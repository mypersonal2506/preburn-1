package secrets

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

const (
	base64URLAlphabet          = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	base62TestAlphabet         = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	randomDraws                = 1000
	distributionDraws          = 8000
	distributionLength         = 32
	distributionToleranceRatio = 0.1
)

func TestNewToken(t *testing.T) {
	seen := make(map[string]bool, randomDraws)
	for range randomDraws {
		token := NewToken()
		if len(token) != 43 {
			t.Fatalf("token=%q length=%d, want 43", token, len(token))
		}
		if strings.Trim(token, base64URLAlphabet) != "" {
			t.Fatalf("token=%q has characters outside base64url", token)
		}
		decoded, err := base64.RawURLEncoding.DecodeString(token)
		if err != nil {
			t.Fatalf("token=%q decode: %v", token, err)
		}
		if len(decoded) != 32 {
			t.Fatalf("token=%q decodes to %d bytes, want 32", token, len(decoded))
		}
		if seen[token] {
			t.Fatalf("token=%q drawn twice", token)
		}
		seen[token] = true
	}
}

func TestHashToken(t *testing.T) {
	const abcDigest = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if digest := hex.EncodeToString(HashToken("abc")); digest != abcDigest {
		t.Errorf("HashToken(abc)=%s, want %s", digest, abcDigest)
	}
	token := NewToken()
	if first, second := HashToken(token), HashToken(token); !bytes.Equal(first, second) {
		t.Errorf("HashToken is unstable: %x then %x", first, second)
	}
	if bytes.Equal(HashToken(token), HashToken(token+"x")) {
		t.Error("HashToken maps two tokens to one digest")
	}
}

func TestRandomBase62LengthAndAlphabet(t *testing.T) {
	for _, length := range []int{0, 1, 4, 32, 257} {
		for range randomDraws {
			value := RandomBase62(length)
			if len(value) != length {
				t.Fatalf("RandomBase62(%d) length=%d", length, len(value))
			}
			if strings.Trim(value, base62TestAlphabet) != "" {
				t.Fatalf("RandomBase62(%d)=%q has characters outside base62", length, value)
			}
		}
	}
}

func TestRandomBase62IsUniform(t *testing.T) {
	counts := make(map[rune]int, len(base62TestAlphabet))
	for range distributionDraws {
		for _, character := range RandomBase62(distributionLength) {
			counts[character]++
		}
	}
	expected := float64(distributionDraws*distributionLength) / float64(len(base62TestAlphabet))
	for _, character := range base62TestAlphabet {
		deviation := (float64(counts[character]) - expected) / expected
		if deviation > distributionToleranceRatio || deviation < -distributionToleranceRatio {
			t.Errorf("character=%c count=%d expected=%.0f", character, counts[character], expected)
		}
	}
}

func TestNewSecretKey(t *testing.T) {
	first := NewSecretKey()
	if len(first) != 44 {
		t.Fatalf("key=%q length=%d, want 44", first, len(first))
	}
	key, err := base64.StdEncoding.DecodeString(first)
	if err != nil {
		t.Fatalf("key=%q decode: %v", first, err)
	}
	if _, err := NewEncryptor(key); err != nil {
		t.Errorf("NewEncryptor rejects a generated key: %v", err)
	}
	if first == NewSecretKey() {
		t.Error("two generated keys are equal")
	}
}
