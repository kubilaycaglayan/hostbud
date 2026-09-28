package llm

import (
	"strings"
	"testing"
)

func TestPreparePaneScrubsSecretsAndCaps(t *testing.T) {
	for _, secret := range []string{"API_KEY=alpha", "Bearer abcdefghijkl", "sk-abcdefgh12345678", "ghp_abcdefgh12345678", "eyJabcdefgh.abcdefgh.abcdefgh", "https://user:pass@example.com/x", "-----BEGIN PRIVATE KEY-----\nsecret\n-----END PRIVATE KEY-----", strings.Repeat("a", 40)} {
		if got := PreparePane("before "+secret+" after", true, ""); strings.Contains(got, secret) {
			t.Errorf("secret retained: %q", secret)
		}
	}
	if got := PreparePane("marker-token", false, "marker-token"); strings.Contains(got, "marker-token") {
		t.Fatal("run token retained when scrub is off")
	}
	got := PreparePane(strings.Repeat("x", 9000), false, "")
	if len(got) != 8192 {
		t.Fatalf("cap=%d", len(got))
	}
}
