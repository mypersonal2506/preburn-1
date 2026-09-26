package secrets

import (
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	passwordMemoryKibibytes = 64 * 1024
	passwordIterations      = 3
	passwordParallelism     = 2
	passwordSaltLength      = 16
	passwordKeyLength       = 32

	argon2Algorithm        = "argon2id"
	argon2Version          = "v=19"
	argon2ParametersFormat = "m=%d,t=%d,p=%d"

	argon2MinimumMemoryPerLane = 8
	argon2MinimumSaltLength    = 8
	argon2MinimumKeyLength     = 4
	argon2MaximumKeyLength     = math.MaxUint32
)

// ErrMalformedPasswordHash reports a stored password hash that is not an
// argon2id PHC string this package can verify.
var ErrMalformedPasswordHash = errors.New("malformed password hash")

type passwordParameters struct {
	memoryKibibytes uint32
	iterations      uint32
	parallelism     uint8
	saltLength      int
	keyLength       uint32
}

type passwordHash struct {
	parameters passwordParameters
	salt       []byte
	key        []byte
}

// HashPassword hashes password with argon2id (64 MiB, 3 iterations,
// parallelism 2, a random 16-byte salt and a 32-byte key) and returns the PHC
// string $argon2id$v=19$m=65536,t=3,p=2$<salt>$<key>.
func HashPassword(password string) string {
	return hashPassword(password, currentPasswordParameters())
}

// VerifyPassword reports whether password matches the PHC string encoded,
// comparing keys in constant time. needsRehash reports whether encoded uses
// parameters other than the ones HashPassword uses now, so the caller stores
// a fresh HashPassword after a match. A string that is not a valid argon2id
// PHC string returns ErrMalformedPasswordHash.
func VerifyPassword(password, encoded string) (matched, needsRehash bool, err error) {
	hash, err := parsePasswordHash(encoded)
	if err != nil {
		return false, false, err
	}
	key := hash.parameters.deriveKey(password, hash.salt)
	matched = subtle.ConstantTimeCompare(key, hash.key) == 1
	return matched, hash.parameters != currentPasswordParameters(), nil
}

// DummyVerify derives a key from password with the current parameters and
// discards it. Login calls it for an unknown email so the response takes as
// long as VerifyPassword against a current hash.
func DummyVerify(password string) {
	parameters := currentPasswordParameters()
	parameters.deriveKey(password, make([]byte, parameters.saltLength))
}

func currentPasswordParameters() passwordParameters {
	return passwordParameters{
		memoryKibibytes: passwordMemoryKibibytes,
		iterations:      passwordIterations,
		parallelism:     passwordParallelism,
		saltLength:      passwordSaltLength,
		keyLength:       passwordKeyLength,
	}
}

func hashPassword(password string, parameters passwordParameters) string {
	salt := randomBytes(parameters.saltLength)
	return strings.Join([]string{
		"",
		argon2Algorithm,
		argon2Version,
		encodePasswordParameters(parameters),
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(parameters.deriveKey(password, salt)),
	}, "$")
}

func encodePasswordParameters(parameters passwordParameters) string {
	return fmt.Sprintf(argon2ParametersFormat, parameters.memoryKibibytes, parameters.iterations, parameters.parallelism)
}

func (parameters passwordParameters) deriveKey(password string, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, parameters.iterations, parameters.memoryKibibytes, parameters.parallelism, parameters.keyLength)
}

func parsePasswordHash(encoded string) (passwordHash, error) {
	fields := strings.Split(encoded, "$")
	if len(fields) != 6 || fields[0] != "" || fields[1] != argon2Algorithm || fields[2] != argon2Version {
		return passwordHash{}, fmt.Errorf("%w: not a PHC string of %s %s", ErrMalformedPasswordHash, argon2Algorithm, argon2Version)
	}
	parametersField, saltField, keyField := fields[3], fields[4], fields[5]
	var parameters passwordParameters
	_, err := fmt.Sscanf(parametersField, argon2ParametersFormat, &parameters.memoryKibibytes, &parameters.iterations, &parameters.parallelism)
	if err != nil || parametersField != encodePasswordParameters(parameters) {
		return passwordHash{}, fmt.Errorf("%w: parameters are not %s", ErrMalformedPasswordHash, argon2ParametersFormat)
	}
	if parameters.iterations < 1 || parameters.parallelism < 1 || parameters.memoryKibibytes < argon2MinimumMemoryPerLane*uint32(parameters.parallelism) {
		return passwordHash{}, fmt.Errorf("%w: parameters below the argon2 minimums", ErrMalformedPasswordHash)
	}
	salt, err := base64.RawStdEncoding.DecodeString(saltField)
	if err != nil || len(salt) < argon2MinimumSaltLength {
		return passwordHash{}, fmt.Errorf("%w: salt is not %d or more bytes of unpadded base64", ErrMalformedPasswordHash, argon2MinimumSaltLength)
	}
	key, err := base64.RawStdEncoding.DecodeString(keyField)
	keyLength := len(key)
	if err != nil || keyLength < argon2MinimumKeyLength || keyLength > argon2MaximumKeyLength {
		return passwordHash{}, fmt.Errorf("%w: key is not %d to %d bytes of unpadded base64", ErrMalformedPasswordHash, argon2MinimumKeyLength, argon2MaximumKeyLength)
	}
	parameters.saltLength = len(salt)
	parameters.keyLength = uint32(keyLength)
	return passwordHash{parameters: parameters, salt: salt, key: key}, nil
}
