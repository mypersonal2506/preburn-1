package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"errors"
	"fmt"
)

// Purpose names what a stored secret is for. Encrypt binds the purpose to the
// ciphertext as associated data, so a ciphertext decrypts only under the
// purpose it was encrypted with.
type Purpose string

// The values are bound into every stored ciphertext, so renaming one makes
// the secrets already stored under it undecryptable.
const (
	// PurposeStripeRestrictedKey is the restricted API key of the Stripe connector.
	PurposeStripeRestrictedKey Purpose = "stripe_restricted_key"
	// PurposeStripeWebhookSecret is the signing secret of the Stripe webhook endpoint.
	PurposeStripeWebhookSecret Purpose = "stripe_webhook_secret" //nolint:gosec // G101: a purpose label, not a credential.
	// PurposeWebhookEndpointSecret is the signing secret of an outgoing webhook endpoint.
	PurposeWebhookEndpointSecret Purpose = "webhook_endpoint_secret"
)

const secretKeySize = 32

// ErrDecryptionFailed reports a ciphertext that does not authenticate under
// the encryptor's key and the given purpose: it was encrypted with another
// key or purpose, or it is truncated or tampered with.
var ErrDecryptionFailed = errors.New("ciphertext does not decrypt under this key and purpose")

// Encryptor encrypts and decrypts stored secrets with AES-256-GCM under the
// installation secret key. It is safe for concurrent use.
type Encryptor struct {
	authenticatedCipher cipher.AEAD
}

// NewEncryptor returns an Encryptor for key, which must be exactly 32 bytes.
func NewEncryptor(key []byte) (*Encryptor, error) {
	if len(key) != secretKeySize {
		return nil, fmt.Errorf("secret key is %d bytes, want %d", len(key), secretKeySize)
	}
	blockCipher, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create aes cipher: %w", err)
	}
	authenticatedCipher, err := cipher.NewGCMWithRandomNonce(blockCipher)
	if err != nil {
		return nil, fmt.Errorf("create gcm cipher: %w", err)
	}
	return &Encryptor{authenticatedCipher: authenticatedCipher}, nil
}

// Encrypt seals plaintext under a random 12-byte nonce with purpose as
// associated data. The result is the nonce followed by the ciphertext and the
// 16-byte authentication tag.
func (encryptor *Encryptor) Encrypt(purpose Purpose, plaintext []byte) []byte {
	return encryptor.authenticatedCipher.Seal(nil, nil, plaintext, []byte(purpose)) //nolint:gosec // G407: NewGCMWithRandomNonce generates the nonce and takes nil.
}

// Decrypt opens a ciphertext produced by Encrypt under the same key and
// purpose. Any other input returns ErrDecryptionFailed.
func (encryptor *Encryptor) Decrypt(purpose Purpose, ciphertext []byte) ([]byte, error) {
	plaintext, err := encryptor.authenticatedCipher.Open(nil, nil, ciphertext, []byte(purpose))
	if err != nil {
		return nil, fmt.Errorf("decrypt %s: %w", purpose, ErrDecryptionFailed)
	}
	return plaintext, nil
}
