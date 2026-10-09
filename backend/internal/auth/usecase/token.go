package usecase

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// tokenBytes is the number of random bytes in a one-time token.
const tokenBytes = 32

// newToken returns a random token for a link, and the hash to store in its place.
func newToken() (token, hash string) {
	b := make([]byte, tokenBytes)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, hashToken(token)
}

// hashToken returns the SHA-256 of token in hex. A token carries 256 random bits, so a
// fast unsalted hash is enough: it cannot be guessed, only looked up.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// wellFormedToken reports whether token could have come from newToken.
func wellFormedToken(token string) bool {
	b, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(b) == tokenBytes
}
