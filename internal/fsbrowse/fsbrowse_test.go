package fsbrowse

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"
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
