package secrets

import (
	"bytes"
	"errors"
	"testing"
)

const encryptionOverhead = 12 + 16

func newTestEncryptor(t *testing.T, keyByte byte) *Encryptor {
	t.Helper()
	encryptor, err := NewEncryptor(bytes.Repeat([]byte{keyByte}, 32))
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}
	return encryptor
}

func TestNewEncryptorRejectsKeySize(t *testing.T) {
	for _, size := range []int{0, 16, 24, 31, 33, 64} {
		if _, err := NewEncryptor(make([]byte, size)); err == nil {
			t.Errorf("NewEncryptor accepted a %d byte key", size)
		}
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	encryptor := newTestEncryptor(t, 1)
	plaintexts := [][]byte{{}, []byte("rk_live_" + RandomBase62(24)), bytes.Repeat([]byte("x"), 4096)}
	purposes := []Purpose{PurposeStripeRestrictedKey, PurposeStripeWebhookSecret, PurposeWebhookEndpointSecret}
	for _, purpose := range purposes {
		for _, plaintext := range plaintexts {
			ciphertext := encryptor.Encrypt(purpose, plaintext)
			if len(ciphertext) != len(plaintext)+encryptionOverhead {
				t.Errorf("purpose=%s ciphertext length=%d, want %d", purpose, len(ciphertext), len(plaintext)+encryptionOverhead)
			}
			decrypted, err := encryptor.Decrypt(purpose, ciphertext)
			if err != nil {
				t.Fatalf("purpose=%s Decrypt: %v", purpose, err)
			}
			if !bytes.Equal(decrypted, plaintext) {
				t.Errorf("purpose=%s decrypted=%q, want %q", purpose, decrypted, plaintext)
			}
		}
	}
}

func TestEncryptUsesFreshNonce(t *testing.T) {
	encryptor := newTestEncryptor(t, 1)
	plaintext := []byte("whsec_" + RandomBase62(32))
	first := encryptor.Encrypt(PurposeStripeWebhookSecret, plaintext)
	second := encryptor.Encrypt(PurposeStripeWebhookSecret, plaintext)
	if bytes.Equal(first[:12], second[:12]) {
		t.Errorf("two encryptions share the nonce %x", first[:12])
	}
	if bytes.Equal(first, second) {
		t.Error("two encryptions of the same plaintext are equal")
	}
}

func TestDecryptRejects(t *testing.T) {
	encryptor := newTestEncryptor(t, 1)
	plaintext := []byte("rk_live_" + RandomBase62(24))
	ciphertext := encryptor.Encrypt(PurposeStripeRestrictedKey, plaintext)
	tampered := func(index int) []byte {
		modified := bytes.Clone(ciphertext)
		modified[index] ^= 0x01
		return modified
	}
	cases := []struct {
		name       string
		encryptor  *Encryptor
		purpose    Purpose
		ciphertext []byte
	}{
		{"another purpose", encryptor, PurposeStripeWebhookSecret, ciphertext},
		{"another key", newTestEncryptor(t, 2), PurposeStripeRestrictedKey, ciphertext},
		{"tampered nonce", encryptor, PurposeStripeRestrictedKey, tampered(0)},
		{"tampered body", encryptor, PurposeStripeRestrictedKey, tampered(12)},
		{"tampered tag", encryptor, PurposeStripeRestrictedKey, tampered(len(ciphertext) - 1)},
		{"truncated by one byte", encryptor, PurposeStripeRestrictedKey, ciphertext[:len(ciphertext)-1]},
		{"truncated to the nonce", encryptor, PurposeStripeRestrictedKey, ciphertext[:12]},
		{"shorter than the nonce", encryptor, PurposeStripeRestrictedKey, ciphertext[:5]},
		{"empty", encryptor, PurposeStripeRestrictedKey, []byte{}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			decrypted, err := testCase.encryptor.Decrypt(testCase.purpose, testCase.ciphertext)
			if !errors.Is(err, ErrDecryptionFailed) {
				t.Fatalf("err=%v, want ErrDecryptionFailed", err)
			}
			if decrypted != nil {
				t.Errorf("decrypted=%q, want nil", decrypted)
			}
		})
	}
}
