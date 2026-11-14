package usecase

import (
	"crypto/hmac"
	"crypto/sha256"
)

// maxTestTokenLen is the longest a token may be: the API refuses longer ones.
const maxTestTokenLen = 200

func newMAC(key, msg []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(msg)
	return m.Sum(nil)
}

// flipFirst changes the first character of a base64url string to another valid one.
func flipFirst(s string) string {
	if s[0] == 'A' {
		return "B" + s[1:]
	}
	return "A" + s[1:]
}
