//go:build integration

package fsbrowse_test

import (
	"context"
	"errors"
	"io"
	"path"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"hostbud/internal/fsbrowse"
	"hostbud/internal/sshx"
	"hostbud/internal/testenv"
)

func TestIntegrationSFTPHomeListStatAndMkdir(t *testing.T) {
	c := testenv.Connected(t, testenv.SSHD)
	svc := fsbrowse.New(c, sshx.HostMachineID, time.Minute, 5*time.Second)
	t.Cleanup(func() { _ = svc.Close() })
	root := "/home/dev/fsbrowse-it"
	special := root + "/space quote ' 雪;$()"
	testenv.Sh(t, c,
		"rm -rf "+sshx.Quote(root)+"; mkdir -p "+sshx.Quote(root+"/z-dir")+" "+sshx.Quote(root+"/a-dir")+" "+sshx.Quote(special)+
			" "+sshx.Quote(root+"/private")+
			"; touch "+sshx.Quote(root+"/.hidden")+" "+sshx.Quote(root+"/z-file")+" "+sshx.Quote(root+"/a-file")+
			"; ln -s a-file "+sshx.Quote(root+"/valid-link")+"; ln -s missing "+sshx.Quote(root+"/broken-link")+
			"; touch "+sshx.Quote(root+"/private/target")+
			"; chmod 000 "+sshx.Quote(root+"/private")+
			"; ln -s private/target "+sshx.Quote(root+"/unreadable-link")+
			"; ln -s loop-b "+sshx.Quote(root+"/loop-a")+"; ln -s loop-a "+sshx.Quote(root+"/loop-b"))
	t.Cleanup(func() { testenv.Sh(t, c, "chmod 700 "+sshx.Quote(root+"/private")+"; rm -rf "+sshx.Quote(root)) })

	home, err := svc.Home(context.Background())
	if err != nil || home != "/home/dev" {
		t.Fatalf("Home() = %q, %v", home, err)
	}
	full, rows, err := svc.List(context.Background(), root, false)
	if err != nil {
		t.Fatal(err)
	}
	if full != root {
		t.Fatalf("listed path = %q", full)
	}
	var names []string
	for _, row := range rows {
		names = append(names, row.Name)
		if row.Kind == "symlink" && row.SymlinkState != "unresolved" {
			t.Errorf("list resolved symlink eagerly: %+v", row)
		}
	}
	want := []string{"a-dir", "private", "space quote ' 雪;$()", "z-dir", "a-file", "broken-link", "loop-a", "loop-b", "unreadable-link", "valid-link", "z-file"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("default listing = %q, want %q", names, want)
	}
	_, hiddenRows, err := svc.List(context.Background(), root, true)
	if err != nil {
		t.Fatal(err)
	}
	if !containsEntry(hiddenRows, ".hidden") {
		t.Fatalf("hidden listing omitted .hidden: %+v", hiddenRows)
	}
	stat, err := svc.Stat(context.Background(), special)
	if err != nil || stat.Kind != "directory" {
		t.Fatalf("stat special path: %+v, %v", stat, err)
	}
	valid, err := svc.Stat(context.Background(), path.Join(root, "valid-link"))
	if err != nil || !valid.Symlink || valid.SymlinkState != "resolved" {
		t.Fatalf("stat valid symlink: %+v, %v", valid, err)
	}
	broken, err := svc.Stat(context.Background(), path.Join(root, "broken-link"))
	if err != nil || broken.SymlinkState != "broken" {
		t.Fatalf("stat broken symlink: %+v, %v", broken, err)
	}
	loop, err := svc.Stat(context.Background(), path.Join(root, "loop-a"))
	if err != nil || loop.SymlinkState != "loop" {
		t.Fatalf("stat looping symlink: %+v, %v", loop, err)
	}
	unreadable, err := svc.Stat(context.Background(), path.Join(root, "unreadable-link"))
	if err != nil || unreadable.SymlinkState != "unreadable" {
		t.Fatalf("stat unreadable symlink: %+v, %v", unreadable, err)
	}
	created, err := svc.Mkdir(context.Background(), special, "new folder 雪;$()")
	if err != nil || created != special+"/new folder 雪;$()" {
		t.Fatalf("Mkdir() = %q, %v", created, err)
	}
	if _, err := svc.Stat(context.Background(), created); err != nil {
		t.Fatalf("stat created child: %v", err)
	}
	for _, bad := range []string{"", ".", "..", "a/b", "a\x00b"} {
		if _, err := svc.Mkdir(context.Background(), special, bad); !errors.Is(err, fsbrowse.ErrInvalidName) {
			t.Errorf("Mkdir name %q error = %v", bad, err)
		}
	}
	if _, _, err := svc.List(context.Background(), root+"/a-file", false); !errors.Is(err, fsbrowse.ErrNotDirectory) {
		t.Fatalf("listing file error = %v", err)
	}
	if _, err := svc.Stat(context.Background(), path.Join(root, "missing")); !fsbrowse.IsNotExist(err) {
		t.Fatalf("Stat missing path error = %v, want not-exist", err)
	}
}

func containsEntry(rows []fsbrowse.Entry, name string) bool {
	for _, row := range rows {
		if row.Name == name {
			return true
		}
	}
	return false
}

func TestIntegrationCancelAndCloseSFTP(t *testing.T) {
	c := testenv.Connected(t, testenv.SSHD)
	svc := fsbrowse.New(c, sshx.HostMachineID, time.Minute, time.Nanosecond)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.Home(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Home() error = %v", err)
	}
	_ = svc.Close()

	// A separately opened normal service exercises subsystem shutdown after a
	// real SFTP handshake; the shared ssh ControlMaster remains usable.
	svc = fsbrowse.New(c, sshx.HostMachineID, time.Minute, 5*time.Second)
	if _, err := svc.Home(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Exec(context.Background(), sshx.HostMachineID, "true"); err != nil {
		t.Fatalf("ssh unusable after SFTP close: %v", err)
	}
}

type countingOpener struct {
	client *sshx.Client
	calls  atomic.Int32
}

func (o *countingOpener) OpenSFTP(ctx context.Context, machine string) (io.ReadWriteCloser, error) {
	o.calls.Add(1)
	return o.client.OpenSFTP(ctx, machine)
}

func TestIntegrationSFTPClientClosesWhenIdle(t *testing.T) {
	c := testenv.Connected(t, testenv.SSHD)
	opener := &countingOpener{client: c}
	svc := fsbrowse.New(opener, sshx.HostMachineID, 40*time.Millisecond, 5*time.Second)
	t.Cleanup(func() { _ = svc.Close() })
	if _, err := svc.Home(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if _, err := svc.Home(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := opener.calls.Load(); got != 2 {
		t.Fatalf("OpenSFTP calls after idle close = %d, want 2", got)
	}
}
