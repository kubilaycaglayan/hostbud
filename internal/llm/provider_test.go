package llm

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func validConfig() Config {
	return Config{Provider: "openai", Model: "test-model", APIKey: "key", QuietAfter: 20 * time.Minute, MaxPerRunHour: 2, Scrub: true}
}
func TestCheckDisabledAndInvalid(t *testing.T) {
	for name, mutate := range map[string]func(*Config){
		"provider": func(c *Config) { c.Provider = "other" }, "model": func(c *Config) { c.Model = "" }, "key": func(c *Config) { c.APIKey = "" },
		"key whitespace": func(c *Config) { c.APIKey = " key " }, "quiet": func(c *Config) { c.QuietAfter = time.Second }, "budget": func(c *Config) { c.MaxPerRunHour = 21 }, "base url": func(c *Config) { c.BaseURL = "http://api.example.com" },
	} {
		t.Run(name, func(t *testing.T) {
			c := validConfig()
			mutate(&c)
			status, err := Check(c)
			if err != nil || status.Enabled || status.Reason == "" {
				t.Fatalf("status=%+v err=%v", status, err)
			}
			if c.APIKey != "" && strings.Contains(status.Reason, c.APIKey) {
				t.Fatal("key leaked in reason")
			}
		})
	}
	status, _ := Check(Config{})
	if status.Enabled || !strings.Contains(status.Reason, "HOSTBUD_LLM_PROVIDER") {
		t.Fatalf("%+v", status)
	}
}
func TestOpenAIMalformedAnswerBecomesUnknown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer marker-secret" {
			t.Error("missing authorization")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\\"label\\":\\"oops\\",\\"reason\\":\\"x\\"}"}}]}`))
	}))
	defer srv.Close()
	c := validConfig()
	c.APIKey = "marker-secret"
	c.BaseURL = srv.URL
	cl := NewOpenAI(c)
	result, err := cl.Classify(t.Context(), "pane marker")
	if err != nil || result.Label != Unknown {
		t.Fatalf("%+v %v", result, err)
	}
}
func TestReasonIsBoundedAndSingleLine(t *testing.T) {
	got := cleanReason(strings.Repeat("a", 205) + "\nsecret")
	if len(got) > 200 || strings.ContainsAny(got, "\n\r") {
		t.Fatalf("reason %q", got)
	}
}
