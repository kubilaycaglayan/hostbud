//go:build integration

package api

import (
	"crypto/sha256"
	"encoding/base64"
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"hostbud/web"
)

func TestBuiltIndexHashMatchesContentSecurityPolicy(t *testing.T) {
	index, err := fs.ReadFile(web.Dist(), "index.html")
	if err != nil {
		t.Fatal(err)
	}
	scripts := regexp.MustCompile(`(?is)<script\b([^>]*)>(.*?)</script\s*>`).FindAllSubmatch(index, -1)
	var boot []byte
	for _, match := range scripts {
		if !regexp.MustCompile(`(?i)\bsrc\s*=`).Match(match[1]) {
			boot = match[2]
		}
	}
	if len(boot) == 0 {
		t.Fatal("built index has no inline theme boot script")
	}
	hash := sha256.Sum256(boot)
	policy, err := BuildContentSecurityPolicy(web.Dist(), AllowedOrigins("hostbud.example.test", 9055))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(policy, "'sha256-"+base64.StdEncoding.EncodeToString(hash[:])+"'") {
		t.Fatalf("built index hash missing from CSP: %s", policy)
	}
}
