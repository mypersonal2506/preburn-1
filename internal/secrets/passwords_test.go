package secrets

import (
	"errors"
	"strings"
	"testing"
)

const (
	testPassword       = "correct horse battery staple"
	currentHashPrefix  = "$argon2id$v=19$m=65536,t=3,p=2$"
	validTestSalt      = "c29tZXNhbHRzb21lc2FsdA"
	validTestKey       = "aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGFzaGhhc2g"
	lowParametersField = "m=64,t=1,p=1"
)

func lowPasswordParameters() passwordParameters {
	return passwordParameters{memoryKibibytes: 64, iterations: 1, parallelism: 1, saltLength: 16, keyLength: 32}
}

func TestHashPasswordRoundTrip(t *testing.T) {
	encoded := HashPassword(testPassword)
	if !strings.HasPrefix(encoded, currentHashPrefix) {
		t.Fatalf("encoded=%q, want prefix %q", encoded, currentHashPrefix)
	}
	matched, needsRehash, err := VerifyPassword(testPassword, encoded)
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if !matched || needsRehash {
		t.Errorf("matched=%t needs_rehash=%t, want matched=true needs_rehash=false", matched, needsRehash)
	}
}

func TestHashPasswordUsesFreshSalt(t *testing.T) {
	first := hashPassword(testPassword, lowPasswordParameters())
	second := hashPassword(testPassword, lowPasswordParameters())
	if first == second {
		t.Errorf("two hashes of one password are equal: %q", first)
	}
}

func TestVerifyPasswordWrongPassword(t *testing.T) {
	encoded := hashPassword(testPassword, lowPasswordParameters())
	for _, attempt := range []string{"", "correct horse battery stapl", testPassword + " ", strings.ToUpper(testPassword)} {
		matched, _, err := VerifyPassword(attempt, encoded)
		if err != nil {
			t.Fatalf("attempt=%q VerifyPassword: %v", attempt, err)
		}
		if matched {
			t.Errorf("attempt=%q matched", attempt)
		}
	}
}

func TestVerifyPasswordNeedsRehashForLowerParameters(t *testing.T) {
	encoded := hashPassword(testPassword, lowPasswordParameters())
	if !strings.HasPrefix(encoded, "$argon2id$v=19$"+lowParametersField+"$") {
		t.Fatalf("encoded=%q does not carry the low parameters", encoded)
	}
	matched, needsRehash, err := VerifyPassword(testPassword, encoded)
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if !matched || !needsRehash {
		t.Errorf("matched=%t needs_rehash=%t, want both true", matched, needsRehash)
	}
}

func TestVerifyPasswordRejectsMalformed(t *testing.T) {
	cases := []struct {
		name    string
		encoded string
	}{
		{"empty", ""},
		{"plain text", "hunter2hunter2"},
		{"no leading separator", "argon2id$v=19$" + lowParametersField + "$" + validTestSalt + "$" + validTestKey},
		{"missing key", "$argon2id$v=19$" + lowParametersField + "$" + validTestSalt},
		{"extra field", "$argon2id$v=19$" + lowParametersField + "$" + validTestSalt + "$" + validTestKey + "$extra"},
		{"argon2i", "$argon2i$v=19$" + lowParametersField + "$" + validTestSalt + "$" + validTestKey},
		{"bcrypt", "$2b$12$R9h/cIPz0gi.URNNX3kh2OPST9/PgBkqquzi.Ss7KIUgO2t0jWMUW"},
		{"old version", "$argon2id$v=16$" + lowParametersField + "$" + validTestSalt + "$" + validTestKey},
		{"version without number", "$argon2id$v=$" + lowParametersField + "$" + validTestSalt + "$" + validTestKey},
		{"parameters out of order", "$argon2id$v=19$t=1,m=64,p=1$" + validTestSalt + "$" + validTestKey},
		{"missing parameter", "$argon2id$v=19$m=64,t=1$" + validTestSalt + "$" + validTestKey},
		{"extra parameter", "$argon2id$v=19$" + lowParametersField + ",k=1$" + validTestSalt + "$" + validTestKey},
		{"non-numeric memory", "$argon2id$v=19$m=lots,t=1,p=1$" + validTestSalt + "$" + validTestKey},
		{"negative iterations", "$argon2id$v=19$m=64,t=-1,p=1$" + validTestSalt + "$" + validTestKey},
		{"zero iterations", "$argon2id$v=19$m=64,t=0,p=1$" + validTestSalt + "$" + validTestKey},
		{"zero parallelism", "$argon2id$v=19$m=64,t=1,p=0$" + validTestSalt + "$" + validTestKey},
		{"parallelism above 255", "$argon2id$v=19$m=4096,t=1,p=256$" + validTestSalt + "$" + validTestKey},
		{"memory above 32 bits", "$argon2id$v=19$m=4294967296,t=1,p=1$" + validTestSalt + "$" + validTestKey},
		{"memory below 8 per lane", "$argon2id$v=19$m=15,t=1,p=2$" + validTestSalt + "$" + validTestKey},
		{"padded salt", "$argon2id$v=19$" + lowParametersField + "$" + validTestSalt + "==$" + validTestKey},
		{"salt outside the alphabet", "$argon2id$v=19$" + lowParametersField + "$c29tZXNhbHRz*21lc2FsdA$" + validTestKey},
		{"salt shorter than 8 bytes", "$argon2id$v=19$" + lowParametersField + "$c2FsdA$" + validTestKey},
		{"empty key", "$argon2id$v=19$" + lowParametersField + "$" + validTestSalt + "$"},
		{"key shorter than 4 bytes", "$argon2id$v=19$" + lowParametersField + "$" + validTestSalt + "$aGFz"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			matched, needsRehash, err := VerifyPassword(testPassword, testCase.encoded)
			if !errors.Is(err, ErrMalformedPasswordHash) {
				t.Fatalf("err=%v, want ErrMalformedPasswordHash", err)
			}
			if matched || needsRehash {
				t.Errorf("matched=%t needs_rehash=%t, want both false", matched, needsRehash)
			}
		})
	}
}

func BenchmarkVerifyPassword(b *testing.B) {
	encoded := HashPassword(testPassword)
	for b.Loop() {
		if _, _, err := VerifyPassword(testPassword, encoded); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDummyVerify(b *testing.B) {
	for b.Loop() {
		DummyVerify(testPassword)
	}
}
