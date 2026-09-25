package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"math/big"
	"time"
)

// newToken returns an opaque, high-entropy session token (256 bits).
func newToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// hashToken is what auth_sessions stores instead of the token.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// newULID returns a ULID: 48-bit millisecond time + 80 random bits, in
// Crockford base32 (26 chars, sortable by creation time).
func newULID(now time.Time) string {
	var b [16]byte
	var ms [8]byte
	binary.BigEndian.PutUint64(ms[:], uint64(now.UnixMilli())) //nolint:gosec // time after 1970
	copy(b[:6], ms[2:])
	_, _ = rand.Read(b[6:])
	// 128 bits → 26 base32 digits (the first carries 3 bits).
	n := new(big.Int).SetBytes(b[:])
	out := make([]byte, 26)
	mask := big.NewInt(31)
	for i := 25; i >= 0; i-- {
		out[i] = crockford[new(big.Int).And(n, mask).Int64()]
		n.Rsh(n, 5)
	}
	return string(out)
}
