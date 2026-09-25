package auth

import (
	"errors"
	"strings"
	"unicode"
)

// ErrInvalidEmail means the address isn't shaped like an email address.
var ErrInvalidEmail = errors.New("enter a valid email address")

// NormalizeEmail trims and case-folds an address (the form used for every
// lookup and in email_allowlist) and checks its basic shape.
func NormalizeEmail(s string) (string, error) {
	e := strings.ToLower(strings.TrimSpace(s))
	local, domain, ok := strings.Cut(e, "@")
	if !ok || local == "" || domain == "" || strings.Contains(domain, "@") || len(e) > 254 ||
		!strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return "", ErrInvalidEmail
	}
	for _, r := range e {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return "", ErrInvalidEmail
		}
	}
	return e, nil
}

// Redact masks an address for logs: "person@example.com" → "p***@example.com".
func Redact(email string) string {
	local, domain, ok := strings.Cut(email, "@")
	if !ok || local == "" {
		return "***"
	}
	return local[:1] + "***@" + domain
}
