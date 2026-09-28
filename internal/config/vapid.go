package config

import (
	"bytes"
	"crypto/ecdh"
	"encoding/base64"
	"net/mail"
	"net/url"
	"strings"
)

// VAPID env var names (V2-M3).
const (
	EnvVAPIDPublicKey  = "HOSTBUD_VAPID_PUBLIC_KEY"
	EnvVAPIDPrivateKey = "HOSTBUD_VAPID_PRIVATE_KEY"
	EnvVAPIDSubject    = "HOSTBUD_VAPID_SUBJECT"
)

// pushOffHint is the fix the owner sees whenever push is off.
const pushOffHint = "set HOSTBUD_VAPID_PUBLIC_KEY, HOSTBUD_VAPID_PRIVATE_KEY and HOSTBUD_VAPID_SUBJECT (make vapid-keys)"

// Push is whether Web Push can be used. Unavailable push is never a
// startup error: in-app notifications still work.
type Push struct {
	Available bool
	// Reason is shown in Settings when push is off.
	Reason string
	// Missing and Invalid name the env vars at fault (never their values).
	Missing []string
	Invalid []string
}

// CheckVAPID validates a VAPID key pair and subject: the public key is an
// uncompressed P-256 point and the private key its 32-byte scalar, both
// base64url; the subject is a mailto: address or an https URL.
func CheckVAPID(public, private, subject string) Push {
	var p Push
	for _, v := range []struct{ name, value string }{{EnvVAPIDPublicKey, public}, {EnvVAPIDPrivateKey, private}, {EnvVAPIDSubject, subject}} {
		if v.value == "" {
			p.Missing = append(p.Missing, v.name)
		}
	}
	if len(p.Missing) > 0 {
		p.Reason = "Push is off: " + pushOffHint
		return p
	}
	pub, errPub := decodeKey(public)
	if errPub != nil || len(pub) != 65 || pub[0] != 4 {
		p.Invalid = append(p.Invalid, EnvVAPIDPublicKey)
	} else if _, err := ecdh.P256().NewPublicKey(pub); err != nil {
		p.Invalid = append(p.Invalid, EnvVAPIDPublicKey)
	}
	priv, errPriv := decodeKey(private)
	var key *ecdh.PrivateKey
	if errPriv == nil && len(priv) == 32 {
		key, errPriv = ecdh.P256().NewPrivateKey(priv)
	}
	if errPriv != nil || key == nil {
		p.Invalid = append(p.Invalid, EnvVAPIDPrivateKey)
	} else if len(p.Invalid) == 0 && !bytes.Equal(key.PublicKey().Bytes(), pub) {
		p.Invalid = append(p.Invalid, EnvVAPIDPrivateKey) // not the public key's pair
	}
	if !validSubject(subject) {
		p.Invalid = append(p.Invalid, EnvVAPIDSubject)
	}
	if len(p.Invalid) > 0 {
		p.Reason = "Push is off: " + strings.Join(p.Invalid, ", ") + " is not valid; " + pushOffHint
		return p
	}
	p.Available = true
	return p
}

func decodeKey(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
}

func validSubject(s string) bool {
	if addr, ok := strings.CutPrefix(s, "mailto:"); ok {
		a, err := mail.ParseAddress(addr)
		return err == nil && a.Address == addr
	}
	u, err := url.Parse(s)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil
}
