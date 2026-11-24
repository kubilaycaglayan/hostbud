package store

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
)

func TestCompareUIStateConcurrentWriters(t *testing.T) {
	s := openTemp(t, t.TempDir())
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	base := json.RawMessage(`{"projects":["a","b"]}`)
	ok, err := s.CompareUIStateForUser(ctx, "user-a", "tree", json.RawMessage(`null`), base)
	if err != nil || !ok {
		t.Fatalf("initial save: %v %v", ok, err)
	}
	var wg sync.WaitGroup
	results := make(chan bool, 2)
	for _, value := range []string{`{"projects":["b","a"]}`, `{"projects":["a","b"],"collapsed":["a"]}`} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := s.CompareUIStateForUser(ctx, "user-a", "tree", base, json.RawMessage(value))
			if err != nil {
				t.Error(err)
			}
			results <- ok
		}()
	}
	wg.Wait()
	close(results)
	wins := 0
	for ok := range results {
		if ok {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("successful competing writes=%d, want 1", wins)
	}
	if ok, err := s.CompareUIStateForUser(ctx, "user-a", "tree", json.RawMessage(`null`), base); err != nil || ok {
		t.Fatalf("stale missing base: %v %v", ok, err)
	}
	if ok, err := s.CompareUIStateForUser(ctx, "user-b", "tree", json.RawMessage(`null`), base); err != nil || !ok {
		t.Fatalf("other account: %v %v", ok, err)
	}
}
