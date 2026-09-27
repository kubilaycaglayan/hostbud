// Package queue runs the v2 agent task queue: the queue service, the
// dispatcher and the run hook receiver (docs/roadmap-v2/ARCHITECTURE.md §4–§9).
package queue

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
)

// NewToken returns a new run bearer token (32 random bytes, base64url) and
// its SHA-256, the only form that is stored.
func NewToken() (token string, hash []byte, err error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", nil, fmt.Errorf("generate run token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(b[:])
	return token, HashToken(token), nil
}

// HashToken returns the SHA-256 of a run token.
func HashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// TokenMatches compares a presented token with a stored hash in constant time.
func TokenMatches(token string, hash []byte) bool {
	return len(hash) == sha256.Size && subtle.ConstantTimeCompare(HashToken(token), hash) == 1
}
