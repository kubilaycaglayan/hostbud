package main

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

const healthcheckURL = "http://127.0.0.1:8080/api/health"
const healthcheckTimeout = 2 * time.Second

func runHealthcheck() error {
	ctx, cancel := context.WithTimeout(context.Background(), healthcheckTimeout)
	defer cancel()
	client := &http.Client{Timeout: healthcheckTimeout}
	return verifyHealth(ctx, client, healthcheckURL)
}

func verifyHealth(ctx context.Context, client *http.Client, endpoint string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("create healthcheck request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("hostbud health endpoint did not answer: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("hostbud health endpoint returned HTTP %d", resp.StatusCode)
	}
	return nil
}
