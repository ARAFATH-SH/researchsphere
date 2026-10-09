package argon2

import (
	"errors"
	"regexp"
	"strings"
	"testing"
)

// fast keeps the tests quick; the format and logic are the same as with DefaultParams.
var fast = Params{Memory: 64, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}

func TestHashFormat(t *testing.T) {
	encoded, err := New(DefaultParams).Hash("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	phc := regexp.MustCompile(`^\$argon2id\$v=19\$m=19456,t=2,p=1\$[A-Za-z0-9+/]{22}\$[A-Za-z0-9+/]{43}$`)
	if !phc.MatchString(encoded) {
		t.Errorf("hash %q is not in the PHC format with the default parameters", encoded)
	}
	if strings.Contains(encoded, "correct horse") {
		t.Error("hash contains the password")
	}
}

func TestHashAndVerify(t *testing.T) {
	h := New(fast)
	encoded, err := h.Hash("pässwörd")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		password string
		want     bool
	}{
		{"same password", "pässwörd", true},
		{"wrong password", "passwörd", false},
		{"other letter case", "PÄSSWÖRD", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := h.Verify(tt.password, encoded)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("Verify() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSamePasswordGetsDifferentSalts(t *testing.T) {
	h := New(fast)
	a, _ := h.Hash("same password")
	b, _ := h.Hash("same password")
	if a == b {
		t.Error("two hashes of one password are equal: the salt is not random")
	}
}

func TestVerifyMalformed(t *testing.T) {
	for _, encoded := range []string{
		"",
		"plain text",
		"$2a$10$bcrypthashbcrypthashbcrypthashbcrypthashbcrypthashbcry",
		"$argon2i$v=19$m=64,t=1,p=1$c2FsdHNhbHRzYWx0c2FsdA$aGFzaA",
		"$argon2id$v=16$m=64,t=1,p=1$c2FsdHNhbHRzYWx0c2FsdA$aGFzaA",
		"$argon2id$v=19$m=64,t=0,p=1$c2FsdHNhbHRzYWx0c2FsdA$aGFzaA",
		"$argon2id$v=19$m=64,t=1,p=1$!!!$aGFzaA",
		"$argon2id$v=19$m=64,t=1,p=1$c2FsdHNhbHRzYWx0c2FsdA$",
	} {
		if _, err := New(fast).Verify("x", encoded); !errors.Is(err, ErrMalformedHash) {
			t.Errorf("Verify(%q) error = %v, want ErrMalformedHash", encoded, err)
		}
	}
}
