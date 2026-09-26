package secrets

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

const (
	tokenSize      = 32
	base62Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	base62Accepted = 256 - 256%len(base62Alphabet)
)

// NewToken returns 32 random bytes encoded as base64url without padding, the
// form of session, link and setup tokens. Store only HashToken of it.
func NewToken() string {
	return base64.RawURLEncoding.EncodeToString(randomBytes(tokenSize))
}

// HashToken returns the SHA-256 digest of token, the form tokens and API key
// secrets are stored and looked up in.
func HashToken(token string) []byte {
	digest := sha256.Sum256([]byte(token))
	return digest[:]
}

// RandomBase62 returns length random characters from 0-9, A-Z and a-z. It
// rejects random bytes at or above the largest multiple of 62, so every
// character is equally likely. It panics if length is negative.
func RandomBase62(length int) string {
	result := make([]byte, 0, length)
	for len(result) < length {
		for _, value := range randomBytes(length - len(result)) {
			index := int(value)
			if index < base62Accepted {
				result = append(result, base62Alphabet[index%len(base62Alphabet)])
			}
		}
	}
	return string(result)
}

// NewSecretKey returns a new installation secret key: 32 random bytes in
// standard base64, the format of PREBURN_SECRET_KEY.
func NewSecretKey() string {
	return base64.StdEncoding.EncodeToString(randomBytes(secretKeySize))
}

func randomBytes(size int) []byte {
	buffer := make([]byte, size)
	rand.Read(buffer)
	return buffer
}
