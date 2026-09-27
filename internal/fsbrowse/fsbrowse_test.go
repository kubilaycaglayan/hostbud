package fsbrowse

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestNormalize(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", "/home/dev"},
		{"~", "/home/dev"},
		{"~/work/app", "/home/dev/work/app"},
		{"work/../app", "/home/dev/app"},
		{"/tmp/a b/雪;$(id)", "/tmp/a b/雪;$(id)"},
		{"//tmp///a", "/tmp/a"},
	}
	for _, tt := range tests {
		got, err := Normalize(tt.in, "/home/dev")
		if err != nil || got != tt.want {
			t.Errorf("Normalize(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
	for _, bad := range []string{"\x00", string(make([]byte, MaxPathBytes+1))} {
		if _, err := Normalize(bad, "/home/dev"); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("Normalize(%q) error = %v, want ErrInvalidPath", bad, err)
		}
	}
	if _, err := Normalize(".", "relative-home"); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("relative home error = %v", err)
	}
}

func TestValidChildName(t *testing.T) {
	for _, bad := range []string{"", ".", "..", "a/b", "a\x00b", string(make([]byte, MaxNameBytes+1))} {
		if err := validChildName(bad); !errors.Is(err, ErrInvalidName) {
			t.Errorf("validChildName(%q) = %v, want ErrInvalidName", bad, err)
		}
	}
	for _, good := range []string{" folder ", "quotes'\"", "雪;$()", `back\slash`} {
		if err := validChildName(good); err != nil {
			t.Errorf("validChildName(%q) = %v", good, err)
		}
	}
}

func TestUploadRejectsInvalidNameAndOversizeBeforeOpeningSFTP(t *testing.T) {
	opener := &unusedOpener{}
	svc := New(opener, "host", time.Minute, time.Second)
	if _, err := svc.Upload(context.Background(), "/home/dev", "../bad.heic", strings.NewReader("x"), 1); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("invalid upload name error = %v, want ErrInvalidName", err)
	}
	if _, err := svc.Upload(context.Background(), "/home/dev", "large.heic", strings.NewReader("x"), MaxUploadBytes+1); !errors.Is(err, ErrUploadTooLarge) {
		t.Fatalf("oversize upload error = %v, want ErrUploadTooLarge", err)
	}
	if opener.opened != 0 {
		t.Fatalf("invalid upload opened SFTP %d times", opener.opened)
	}
}

func TestSortEntriesDirectoryFirstThenName(t *testing.T) {
	rows := []Entry{
		{Name: "z-file", Kind: "file"},
		{Name: "z-dir", Kind: "directory"},
		{Name: "a-file", Kind: "file"},
		{Name: "a-dir", Kind: "directory"},
		{Name: "link", Kind: "symlink"},
	}
	sortEntries(rows)
	got := make([]string, len(rows))
	for i, row := range rows {
		got[i] = row.Name
	}
	want := []string{"a-dir", "z-dir", "a-file", "link", "z-file"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sorted names = %v, want %v", got, want)
	}
}

func TestBoundedListingHelpers(t *testing.T) {
	rows := make([]Entry, 2000)
	for i := range rows {
		rows[i] = Entry{Name: strings.Repeat("雪", 400), Path: "/" + strings.Repeat("p", 2000)}
	}
	bounded, truncated, err := boundEntries("/home/dev", rows)
	if err != nil || !truncated || len(bounded) >= len(rows) {
		t.Fatalf("bounded list len=%d truncated=%t err=%v", len(bounded), truncated, err)
	}
	if len(bounded[0].Name) > MaxResponseName || !utf8.ValidString(bounded[0].Name) {
		t.Fatalf("truncated name is invalid: bytes=%d valid=%t", len(bounded[0].Name), utf8.ValidString(bounded[0].Name))
	}
	body, err := json.Marshal(map[string]any{"path": "/home/dev", "entries": bounded, "truncated": truncated})
	if err != nil || len(body)+1 > MaxResponseBytes {
		t.Fatalf("serialized response bytes=%d err=%v", len(body)+1, err)
	}
}

func TestOperationSlotsBoundConcurrencyAndFreeOnCancel(t *testing.T) {
	svc := New(&unusedOpener{}, "host", time.Minute, time.Second)
	for range MaxConcurrentOps {
		if err := svc.acquireSlot(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	deadline, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	if err := svc.acquireSlot(deadline); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("fifth operation = %v, want timeout", err)
	}
	stop()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- svc.acquireSlot(ctx) }()
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("fifth operation = %v", err)
	}
	svc.releaseSlot()
	if err := svc.acquireSlot(context.Background()); err != nil {
		t.Fatalf("slot wasn't freed: %v", err)
	}
	for range MaxConcurrentOps {
		svc.releaseSlot()
	}
}

type unusedOpener struct{ opened int }

func (u *unusedOpener) OpenSFTP(context.Context, string) (io.ReadWriteCloser, error) {
	u.opened++
	return nil, errors.New("unexpected open")
}

type blockingOpener struct {
	started chan struct{}
	stopped chan struct{}
}

func (b *blockingOpener) OpenSFTP(ctx context.Context, _ string) (io.ReadWriteCloser, error) {
	close(b.started)
	<-ctx.Done()
	close(b.stopped)
	return nil, ctx.Err()
}

func TestCanceledRequestDoesNotOpenSFTP(t *testing.T) {
	opener := &unusedOpener{}
	svc := New(opener, "host", 0, 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.Home(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Home error = %v, want canceled", err)
	}
	if opener.opened != 0 {
		t.Fatalf("opened SFTP %d times for canceled request", opener.opened)
	}
	_ = svc.Close()
}

func TestCanceledRequestInterruptsSFTPHandshake(t *testing.T) {
	opener := &blockingOpener{started: make(chan struct{}), stopped: make(chan struct{})}
	svc := New(opener, "host", 0, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := svc.Home(ctx)
		result <- err
	}()
	<-opener.started
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Home error = %v, want canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("request cancellation did not interrupt SFTP handshake")
	}
	select {
	case <-opener.stopped:
	case <-time.After(time.Second):
		t.Fatal("subsystem opener did not observe cancellation")
	}
	_ = svc.Close()
}

func TestOperationTimeoutInterruptsSFTPHandshake(t *testing.T) {
	opener := &blockingOpener{started: make(chan struct{}), stopped: make(chan struct{})}
	svc := New(opener, "host", time.Minute, 20*time.Millisecond)
	start := time.Now()
	_, err := svc.Home(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
		t.Fatalf("Home error = %v after %v, want bounded deadline", err, time.Since(start))
	}
	select {
	case <-opener.stopped:
	case <-time.After(time.Second):
		t.Fatal("operation timeout did not cancel SFTP subsystem startup")
	}
	_ = svc.Close()
}
