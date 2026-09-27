package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strings"
	"time"
)

type sessionsResponse struct {
	Sessions json.RawMessage `json:"sessions"`
}

type machine struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type machinesResponse struct {
	Machines []machine `json:"machines"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "read-only image check:", err)
		os.Exit(1)
	}
}

func run() error {
	const base = "http://hostbud-test-readonly-app:8080"
	email := os.Getenv("HOSTBUD_TEST_EMAIL")
	password := os.Getenv("HOSTBUD_TEST_PASSWORD")
	if email == "" || password == "" {
		return fmt.Errorf("HOSTBUD_TEST_EMAIL and HOSTBUD_TEST_PASSWORD are required")
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return err
	}
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for _, path := range []string{"/api/auth/register", "/api/auth/login"} {
		body := fmt.Sprintf(`{"email":%q,"password":%q}`, email, password)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, strings.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://localhost:9055")
		resp, err := client.Do(req) // #nosec G704 -- the endpoint is the fixed disposable app on the private test network.
		if err != nil {
			return fmt.Errorf("%s request: %w", path, err)
		}
		_ = resp.Body.Close()
		want := http.StatusCreated
		if path == "/api/auth/login" {
			want = http.StatusOK
		}
		if resp.StatusCode != want {
			return fmt.Errorf("%s returned HTTP %d, want %d", path, resp.StatusCode, want)
		}
	}
	var host machine
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/machines", nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("machine list request: %w", err)
		}
		var result machinesResponse
		decodeErr := json.NewDecoder(resp.Body).Decode(&result)
		_ = resp.Body.Close()
		if decodeErr != nil {
			return fmt.Errorf("decode machine list: %w", decodeErr)
		}
		for _, candidate := range result.Machines {
			if candidate.ID == "host" {
				host = candidate
				break
			}
		}
		if host.Status == "ok" {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if host.ID == "" || host.Status != "ok" {
		return fmt.Errorf("disposable SSH target status is %q, want ok", host.Status)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/machines/host/sessions", nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("session list request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("session list returned HTTP %d, want 200", resp.StatusCode)
	}
	var result sessionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode session list: %w", err)
	}
	if len(result.Sessions) == 0 {
		return fmt.Errorf("session list response has no sessions field")
	}
	return nil
}
