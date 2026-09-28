package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func validConfig() Config {
	return Config{Provider: "openai", Model: "test-model", APIKey: "key", QuietAfter: 20 * time.Minute, MaxPerRunHour: 2, Scrub: true, ScrubValid: true}
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

func TestStatusIncludesExplicitScrubSettingWhenDisabled(t *testing.T) {
	got, err := json.Marshal(Status{Enabled: true, Provider: "openai", Model: "test", Scrub: false, QuietAfter: "20m", MaxPerRunHour: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"scrub":false`) {
		t.Fatalf("status omitted explicit scrub setting: %s", got)
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

func TestOpenAIFailureResponsesAreBounded(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		status        int
		wantCalls     int
		wantReason    string
		wantErr       bool
	}{
		{name: "bad request", status: http.StatusBadRequest, content: `{"error":"bad"}`, wantCalls: 1},
		{name: "unauthorized", status: http.StatusUnauthorized, content: `{"error":"bad"}`, wantCalls: 1, wantReason: "check OPENAI_API_KEY"},
		{name: "429 exhausted", status: http.StatusTooManyRequests, content: `{"error":"busy"}`, wantCalls: 3, wantErr: true},
		{name: "5xx exhausted", status: http.StatusServiceUnavailable, content: `{"error":"busy"}`, wantCalls: 3, wantErr: true},
		{name: "malformed content", status: http.StatusOK, content: `not json`, wantCalls: 1},
		{name: "out of schema", status: http.StatusOK, content: `{"label":"done","reason":"x"}`, wantCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(tc.status)
				if tc.status == http.StatusOK {
					body, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": tc.content}}}})
					_, _ = w.Write(body)
				} else {
					_, _ = w.Write([]byte(tc.content))
				}
			}))
			defer srv.Close()
			c := validConfig()
			c.BaseURL = srv.URL
			result, err := NewOpenAI(c).Classify(t.Context(), "pane")
			if (err != nil) != tc.wantErr || result.Label != Unknown {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if calls != tc.wantCalls {
				t.Fatalf("calls=%d want=%d", calls, tc.wantCalls)
			}
			if tc.wantReason != "" && !strings.Contains(result.Reason, tc.wantReason) {
				t.Fatalf("reason=%q want substring %q", result.Reason, tc.wantReason)
			}
		})
	}
}

func TestOpenAICanceledRequestReturnsUnknownWithoutRetry(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		<-r.Context().Done()
	}))
	defer srv.Close()
	c := validConfig()
	c.BaseURL = srv.URL
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result, err := NewOpenAI(c).Classify(ctx, "pane")
	if err == nil || result.Label != Unknown || calls != 0 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, calls)
	}
}

func TestOpenAITimedOutRequestReturnsUnknownWithoutRetry(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"label\":\"running\",\"reason\":\"ok\"}"}}]}`))
	}))
	defer srv.Close()
	c := validConfig()
	c.BaseURL = srv.URL
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancel()
	result, err := NewOpenAI(c).Classify(ctx, "pane")
	close(release)
	if err == nil || result.Label != Unknown {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("provider request did not start")
	}
}

func TestReasonIsBoundedAndSingleLine(t *testing.T) {
	got := cleanReason(strings.Repeat("a", 205) + "\nsecret")
	if len(got) > 200 || strings.ContainsAny(got, "\n\r") {
		t.Fatalf("reason %q", got)
	}
}
