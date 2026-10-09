// Package argon2 implements the auth domain's PasswordHasher with Argon2id (NFR-5).
// Hashes are stored in the PHC string format, so the parameters can change later
// without breaking existing hashes:
//
//	$argon2id$v=19$m=19456,t=2,p=1$<salt>$<hash>
package argon2

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
)

// Params are the Argon2id settings.
type Params struct {
	Memory  uint32 // KiB
	Time    uint32 // passes
	Threads uint8
	SaltLen int
	KeyLen  uint32
}

// DefaultParams is OWASP's baseline for Argon2id: 19 MiB of memory, 2 passes, 1 thread.
var DefaultParams = Params{Memory: 19 * 1024, Time: 2, Threads: 1, SaltLen: 16, KeyLen: 32}

// ErrMalformedHash is returned by Verify for a string that is not an Argon2id hash.
var ErrMalformedHash = errors.New("argon2: malformed hash")

var b64 = base64.RawStdEncoding

// Hasher hashes passwords with Argon2id and a random salt per password.
type Hasher struct {
	p Params
}

var _ domain.PasswordHasher = Hasher{}

// New returns a Hasher with p.
func New(p Params) Hasher { return Hasher{p: p} }

// Hash returns the PHC string for password.
func (h Hasher) Hash(password string) (string, error) {
	salt := make([]byte, h.p.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("argon2: read salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, h.p.Time, h.p.Memory, h.p.Threads, h.p.KeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, h.p.Memory, h.p.Time, h.p.Threads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// Verify reports whether password matches encoded, using the parameters stored in
// encoded. Login (slice 8) uses it.
func (h Hasher) Verify(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false, ErrMalformedHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, ErrMalformedHash
	}
	var p Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Time, &p.Threads); err != nil || p.Time == 0 || p.Threads == 0 {
		return false, ErrMalformedHash
	}
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return false, ErrMalformedHash
	}
	want, err := b64.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false, ErrMalformedHash
	}
	got := argon2.IDKey([]byte(password), salt, p.Time, p.Memory, p.Threads, uint32(len(want))) //nolint:gosec // len(want) is a stored key length, far below 2^32
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
