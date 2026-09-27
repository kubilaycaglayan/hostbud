// Package fsbrowse exposes a deliberately small SFTP surface for browsing the
// active host. Paths are handled by SFTP and never become shell commands.
package fsbrowse

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/pkg/sftp"
)

const (
	DefaultIdleTimeout = time.Minute
	DefaultOpTimeout   = 10 * time.Second
	MaxPathBytes       = 4096
	MaxEntries         = 2000
	MaxNameBytes       = 255
	MaxResponseBytes   = 1 << 20
	MaxResponseName    = 1024
	MaxConcurrentOps   = 4
	MaxUploadBytes     = 100 << 20
)

var ErrInvalidPath = errors.New("invalid filesystem path")
var ErrInvalidName = errors.New("invalid directory name")
var ErrTooManyEntries = errors.New("directory contains too many entries")
var ErrNotDirectory = errors.New("path is not a directory")
var ErrAlreadyExists = errors.New("path already exists")
var ErrUploadTooLarge = errors.New("upload exceeds the file size limit")
var ErrFileSizeMismatch = errors.New("uploaded byte count does not match the selected file")

// SubsystemOpener starts a protocol subsystem over the configured system ssh
// client. sshx.Client implements it with `ssh -F ... -s sftp`.
type SubsystemOpener interface {
	OpenSFTP(context.Context, string) (io.ReadWriteCloser, error)
}

// Entry is a safe directory row. Symlinks are not followed by list; callers
// use Stat for the selected link when they need its target state.
type Entry struct {
	Name         string    `json:"name"`
	Path         string    `json:"path"`
	Kind         string    `json:"kind"`
	Size         int64     `json:"size"`
	ModifiedAt   time.Time `json:"modifiedAt"`
	SymlinkState string    `json:"symlinkState,omitempty"`
}

// StatResult describes the link itself and, for symlinks, its resolved target
// state. The target is followed only for an explicit stat request.
type StatResult struct {
	Entry
	Symlink bool `json:"symlink"`
}

// Service keeps one lazy SFTP client and closes it after an idle period.
type Service struct {
	opener        SubsystemOpener
	machine       string
	idle          time.Duration
	timeout       time.Duration
	uploadTimeout time.Duration
	ctx           context.Context
	cancel        context.CancelFunc

	mu         sync.Mutex
	client     *sftp.Client
	pipe       io.ReadWriteCloser
	connCancel context.CancelFunc
	idleTimer  *time.Timer
	closed     bool
	lastUsed   time.Time
	active     int
	slots      chan struct{}
}

// New creates the SFTP browser for machine. Timeouts use safe defaults when
// options are non-positive. machine remains explicit for future multi-host use.
func New(opener SubsystemOpener, machine string, idleTimeout, operationTimeout time.Duration, uploadTimeout ...time.Duration) *Service {
	if idleTimeout <= 0 {
		idleTimeout = DefaultIdleTimeout
	}
	if operationTimeout <= 0 {
		operationTimeout = DefaultOpTimeout
	}
	ctx, cancel := context.WithCancel(context.Background())
	uploadDeadline := 5 * time.Minute
	if len(uploadTimeout) > 0 && uploadTimeout[0] > 0 {
		uploadDeadline = uploadTimeout[0]
	}
	return &Service{opener: opener, machine: machine, idle: idleTimeout, timeout: operationTimeout, uploadTimeout: uploadDeadline, ctx: ctx, cancel: cancel, slots: make(chan struct{}, MaxConcurrentOps)}
}

// OperationTimeout reports the per-operation deadline configured for this service.
func (s *Service) OperationTimeout() time.Duration { return s.timeout }

// UploadTimeout reports the byte-transfer deadline configured for this service.
func (s *Service) UploadTimeout() time.Duration { return s.uploadTimeout }

// Close releases the SFTP subsystem and its ssh process.
func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.cancel()
	if s.idleTimer != nil {
		s.idleTimer.Stop()
	}
	s.closeLocked()
	return nil
}

func (s *Service) closeLocked() {
	if s.pipe != nil {
		_ = s.pipe.Close()
		s.pipe = nil
	}
	if s.connCancel != nil {
		s.connCancel()
		s.connCancel = nil
	}
	if s.client != nil {
		_ = s.client.Close()
		s.client = nil
	}
}

func (s *Service) ensureLocked(ctx context.Context) error {
	if s.closed {
		return errors.New("filesystem browser is closed")
	}
	if s.client != nil {
		return nil
	}
	type opened struct {
		pipe   io.ReadWriteCloser
		client *sftp.Client
		err    error
	}
	connCtx, connCancel := context.WithCancel(s.ctx)
	ready := make(chan opened, 1)
	go func() {
		pipe, err := s.opener.OpenSFTP(connCtx, s.machine)
		if err != nil {
			ready <- opened{err: fmt.Errorf("start SFTP subsystem: %w", err)}
			return
		}
		client, err := sftp.NewClientPipe(pipe, pipe)
		if err != nil {
			_ = pipe.Close()
			ready <- opened{err: fmt.Errorf("start SFTP client: %w", err)}
			return
		}
		ready <- opened{pipe: pipe, client: client}
	}()
	var result opened
	select {
	case result = <-ready:
	case <-ctx.Done():
		connCancel()
		result = <-ready
		if result.pipe != nil {
			_ = result.client.Close()
			_ = result.pipe.Close()
		}
		return ctx.Err()
	}
	if result.err != nil {
		connCancel()
		return result.err
	}
	s.pipe, s.client, s.connCancel = result.pipe, result.client, connCancel
	return nil
}

func (s *Service) touchLocked() {
	if s.idleTimer != nil {
		s.idleTimer.Stop()
	}
	s.idleTimer = time.AfterFunc(s.idle, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.active > 0 {
			s.touchLocked()
		} else if s.client != nil && time.Since(s.lastUsed) >= s.idle {
			s.closeLocked()
		}
	})
}

// withClient bounds each operation. Cancellation closes the subsystem stream,
// which interrupts an in-flight SFTP packet and lets the next request reconnect.
func (s *Service) withClient(ctx context.Context, op string, fn func(*sftp.Client) error) error {
	return s.withClientTimeout(ctx, op, s.timeout, fn)
}

func (s *Service) withClientTimeout(ctx context.Context, op string, timeout time.Duration, fn func(*sftp.Client) error) error {
	if ctx == nil {
		ctx = s.ctx
	}
	opCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := s.acquireSlot(opCtx); err != nil {
		var timeoutDuration time.Duration
		if errors.Is(err, context.DeadlineExceeded) {
			timeoutDuration = timeout
		}
		return &Error{Op: op, Err: err, Timeout: timeoutDuration}
	}
	defer s.releaseSlot()
	// A pooled connection can be dead (the host dropped it while idle): the
	// first failure closes it, and one retry on a fresh connection keeps the
	// user from seeing that as an error.
	// Creating a directory isn't retried: it may have happened before the
	// connection broke.
	for {
		reused, err := s.attempt(opCtx, op, timeout, fn)
		var e *Error
		if !reused || op == opMkdir || op == opUpload || !errors.As(err, &e) || !brokenConnection(e.Err) || opCtx.Err() != nil {
			return err
		}
	}
}

// Upload writes the exact supplied bytes to one new file in an existing
// directory. It stages beside the destination and renames only after the
// complete byte count is written, so interrupted uploads don't leave a partial
// file with the requested name. Existing names are never overwritten.
func (s *Service) Upload(ctx context.Context, directory, name string, source io.Reader, size int64) (string, error) {
	if ctx == nil {
		ctx = s.ctx
	}
	if err := validChildName(name); err != nil {
		return "", err
	}
	if source == nil || size < 0 || size > MaxUploadBytes {
		return "", ErrUploadTooLarge
	}
	home, err := s.Home(ctx)
	if err != nil {
		return "", err
	}
	dir, err := Normalize(directory, home)
	if err != nil {
		return "", err
	}
	target := path.Join(dir, name)
	var temp string
	err = s.withClientTimeout(ctx, opUpload, s.uploadTimeout, func(c *sftp.Client) error {
		info, err := c.Stat(dir)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return ErrNotDirectory
		}
		if _, err := c.Lstat(target); err == nil {
			return ErrAlreadyExists
		} else if !IsNotExist(err) {
			return err
		}
		random := make([]byte, 16)
		if _, err := rand.Read(random); err != nil {
			return err
		}
		temp = path.Join(dir, ".hostbud-upload-"+hex.EncodeToString(random))
		remote, err := c.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
		if err != nil {
			return err
		}
		written, copyErr := io.Copy(remote, io.LimitReader(source, MaxUploadBytes+1))
		closeErr := remote.Close()
		if copyErr != nil {
			return copyErr
		}
		if written > MaxUploadBytes {
			return ErrUploadTooLarge
		}
		if written != size {
			return ErrFileSizeMismatch
		}
		if closeErr != nil {
			return closeErr
		}
		if _, err := c.Lstat(target); err == nil {
			return ErrAlreadyExists
		} else if !IsNotExist(err) {
			return err
		}
		if err := c.Rename(temp, target); err != nil {
			// SFTP servers commonly report a generic failure when a concurrent
			// rename finds that another upload has already created the target.
			// Translate that race to the same conflict returned by the preflight
			// checks instead of exposing an opaque transfer error.
			if _, statErr := c.Lstat(target); statErr == nil {
				return ErrAlreadyExists
			}
			return err
		}
		return nil
	})
	if temp != "" {
		if err != nil {
			cleanupCtx := context.WithoutCancel(ctx)
			_ = s.withClientTimeout(cleanupCtx, "remove incomplete upload", s.uploadTimeout, func(c *sftp.Client) error {
				removeErr := c.Remove(temp)
				if IsNotExist(removeErr) {
					return nil
				}
				return removeErr
			})
		}
	}
	if err != nil {
		return "", err
	}
	return target, nil
}

const opMkdir = "create directory"
const opUpload = "upload file"

// brokenConnection reports errors that close the SFTP connection itself
// (not an SFTP status such as permission denied, nor a timeout).
func brokenConnection(err error) bool {
	var statusErr *sftp.StatusError
	return !errors.As(err, &statusErr) && !errors.Is(err, ErrNotDirectory) &&
		!errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled)
}

// attempt runs fn once; reused reports whether it ran on an existing connection.
func (s *Service) attempt(opCtx context.Context, op string, timeoutBound time.Duration, fn func(*sftp.Client) error) (bool, error) {
	s.mu.Lock()
	if err := opCtx.Err(); err != nil {
		s.mu.Unlock()
		var timeout time.Duration
		if errors.Is(err, context.DeadlineExceeded) {
			timeout = timeoutBound
		}
		return false, &Error{Op: op, Err: err, Timeout: timeout}
	}
	reused := s.client != nil
	if err := s.ensureLocked(opCtx); err != nil {
		s.mu.Unlock()
		if opCtx.Err() != nil {
			return false, &Error{Op: op, Err: opCtx.Err(), Timeout: timeoutBound}
		}
		return false, &Error{Op: op, Err: err}
	}
	client, pipe := s.client, s.pipe
	s.active++
	s.lastUsed = time.Now()
	s.touchLocked()
	s.mu.Unlock()
	stop := context.AfterFunc(opCtx, func() {
		s.mu.Lock()
		if s.pipe == pipe {
			s.closeLocked()
		}
		s.mu.Unlock()
	})
	err := fn(client)
	stop()
	s.mu.Lock()
	s.active--
	if opCtx.Err() != nil {
		err = opCtx.Err()
		if s.pipe == pipe {
			s.closeLocked()
		}
	}
	if err != nil {
		var statusErr *sftp.StatusError
		if !errors.As(err, &statusErr) && !errors.Is(err, ErrNotDirectory) && s.pipe == pipe {
			s.closeLocked()
		}
		s.mu.Unlock()
		var timeout time.Duration
		if errors.Is(err, context.DeadlineExceeded) {
			timeout = timeoutBound
		}
		return reused, &Error{Op: op, Err: err, Timeout: timeout}
	}
	s.lastUsed = time.Now()
	s.touchLocked()
	s.mu.Unlock()
	return reused, nil
}

func (s *Service) acquireSlot(ctx context.Context) error {
	select {
	case s.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) releaseSlot() { <-s.slots }

// Error preserves the operation and underlying SFTP/ssh failure for the API.
type Error struct {
	Op      string
	Err     error
	Timeout time.Duration
}

func (e *Error) Error() string { return e.Op + ": " + e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

// IsNotExist reports SFTP no-such-file statuses as well as local not-exist
// errors, without exposing pkg/sftp's status representation to API callers.
func IsNotExist(err error) bool {
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	var statusErr *sftp.StatusError
	return errors.As(err, &statusErr) && statusErr.FxCode() == sftp.ErrSSHFxNoSuchFile
}

// IsPermission reports SFTP permission-denied statuses and local errors.
func IsPermission(err error) bool {
	if errors.Is(err, os.ErrPermission) {
		return true
	}
	var statusErr *sftp.StatusError
	return errors.As(err, &statusErr) && statusErr.FxCode() == sftp.ErrSSHFxPermissionDenied
}

// Normalize converts empty, home-relative and relative paths to clean absolute
// target paths. It uses POSIX path rules independent of the app container OS.
func Normalize(value, home string) (string, error) {
	if len(value) > MaxPathBytes || strings.IndexByte(value, 0) >= 0 || home == "" || !path.IsAbs(home) {
		return "", ErrInvalidPath
	}
	if value == "" || value == "~" {
		value = home
	} else if value == "~/" || strings.HasPrefix(value, "~/") {
		value = path.Join(home, strings.TrimPrefix(value, "~/"))
	} else if !path.IsAbs(value) {
		value = path.Join(home, value)
	}
	clean := path.Clean(value)
	if len(clean) > MaxPathBytes || !path.IsAbs(clean) {
		return "", ErrInvalidPath
	}
	return clean, nil
}

func validChildName(name string) error {
	if name == "" || name == "." || name == ".." || len(name) > MaxNameBytes ||
		strings.Contains(name, "/") || strings.IndexByte(name, 0) >= 0 {
		return ErrInvalidName
	}
	return nil
}

func kind(info os.FileInfo) string {
	switch mode := info.Mode(); {
	case mode&os.ModeSymlink != 0:
		return "symlink"
	case mode.IsDir():
		return "directory"
	case mode.IsRegular():
		return "file"
	default:
		return "other"
	}
}

func entry(name, full string, info os.FileInfo) Entry {
	e := Entry{Name: name, Path: full, Kind: kind(info), Size: info.Size(), ModifiedAt: info.ModTime().UTC()}
	if e.Kind == "symlink" {
		e.SymlinkState = "unresolved"
	}
	return e
}

func sortEntries(rows []Entry) {
	sort.Slice(rows, func(i, j int) bool {
		di, dj := rows[i].Kind == "directory", rows[j].Kind == "directory"
		if di != dj {
			return di
		}
		return rows[i].Name < rows[j].Name
	})
}

func (s *Service) Home(ctx context.Context) (string, error) {
	var home string
	err := s.withClient(ctx, "read SFTP home", func(c *sftp.Client) error {
		var err error
		home, err = c.Getwd()
		return err
	})
	if err != nil {
		return "", err
	}
	return Normalize(home, home)
}

func (s *Service) List(ctx context.Context, value string, hidden bool) (string, []Entry, error) {
	full, rows, _, err := s.ListPage(ctx, value, hidden)
	return full, rows, err
}

// ListPage returns a bounded page and reports whether more directory entries
// were omitted by the entry or JSON response limit.
func (s *Service) ListPage(ctx context.Context, value string, hidden bool) (string, []Entry, bool, error) {
	home, err := s.Home(ctx)
	if err != nil {
		return "", nil, false, err
	}
	full, err := Normalize(value, home)
	if err != nil {
		return "", nil, false, err
	}
	var rows []Entry
	truncated := false
	err = s.withClient(ctx, "list directory", func(c *sftp.Client) error {
		info, err := c.Stat(full)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return ErrNotDirectory
		}
		infos, err := c.ReadDir(full)
		if err != nil {
			return err
		}
		rows = make([]Entry, 0, len(infos))
		allRows := rows
		for _, info := range infos {
			name := info.Name()
			if name == "." || name == ".." || (!hidden && strings.HasPrefix(name, ".")) {
				continue
			}
			allRows = append(allRows, entry(name, path.Join(full, name), info))
		}
		rows, truncated, err = boundEntries(full, allRows)
		return err
	})
	return full, rows, truncated, err
}

func boundEntries(full string, rows []Entry) ([]Entry, bool, error) {
	sortEntries(rows)
	truncated := false
	for i := range rows {
		if len(rows[i].Name) > MaxResponseName {
			rows[i].Name = truncateUTF8(rows[i].Name, MaxResponseName)
			truncated = true
		}
	}
	if len(rows) > MaxEntries {
		rows = rows[:MaxEntries]
		truncated = true
	}
	bounded := rows[:0]
	used := listingResponseBase(full)
	for _, row := range rows {
		encoded, err := json.Marshal(row)
		if err != nil {
			return nil, truncated, err
		}
		extra := len(encoded)
		if len(bounded) > 0 {
			extra++
		}
		if used+extra > MaxResponseBytes {
			truncated = true
			break
		}
		bounded = append(bounded, row)
		used += extra
	}
	return bounded, truncated, nil
}

func listingResponseBase(full string) int {
	pathJSON, _ := json.Marshal(full)
	return len(`{"entries":[`) + len(`],"path":`) + len(pathJSON) + len(`,"truncated":true}`) + 1
}

func truncateUTF8(value string, limit int) string {
	value = strings.ToValidUTF8(value, "�")
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func (s *Service) Stat(ctx context.Context, value string) (StatResult, error) {
	home, err := s.Home(ctx)
	if err != nil {
		return StatResult{}, err
	}
	full, err := Normalize(value, home)
	if err != nil {
		return StatResult{}, err
	}
	var result StatResult
	err = s.withClient(ctx, "stat path", func(c *sftp.Client) error {
		info, err := c.Lstat(full)
		if err != nil {
			return err
		}
		result = StatResult{Entry: entry(path.Base(full), full, info), Symlink: info.Mode()&os.ModeSymlink != 0}
		if !result.Symlink {
			return nil
		}
		result.SymlinkState = resolveSymlink(c, full)
		return nil
	})
	return result, err
}

func resolveSymlink(c *sftp.Client, link string) string {
	current := link
	seen := make(map[string]struct{})
	for range 40 {
		if _, ok := seen[current]; ok {
			return "loop"
		}
		seen[current] = struct{}{}
		info, err := c.Lstat(current)
		if err != nil {
			if IsNotExist(err) {
				return "broken"
			}
			return "unreadable"
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return "resolved"
		}
		target, err := c.ReadLink(current)
		if err != nil {
			if IsNotExist(err) {
				return "broken"
			}
			return "unreadable"
		}
		if path.IsAbs(target) {
			current = path.Clean(target)
		} else {
			current = path.Clean(path.Join(path.Dir(current), target))
		}
	}
	return "loop"
}

func (s *Service) Mkdir(ctx context.Context, parent, name string) (string, error) {
	if err := validChildName(name); err != nil {
		return "", err
	}
	home, err := s.Home(ctx)
	if err != nil {
		return "", err
	}
	dir, err := Normalize(parent, home)
	if err != nil {
		return "", err
	}
	child := path.Join(dir, name)
	if child == dir || !strings.HasPrefix(child, strings.TrimSuffix(dir, "/")+"/") {
		return "", ErrInvalidName
	}
	err = s.withClient(ctx, opMkdir, func(c *sftp.Client) error {
		parentInfo, err := c.Stat(dir)
		if err != nil {
			return err
		}
		if !parentInfo.IsDir() {
			return ErrNotDirectory
		}
		if _, err := c.Lstat(child); err == nil {
			return ErrAlreadyExists
		} else if !IsNotExist(err) {
			return err
		}
		return c.Mkdir(child)
	})
	if err != nil {
		return "", err
	}
	return child, nil
}

// RealPath resolves an absolute path's symlinks on the host (V2-M1: the
// agent adapters check a transcript still lies under the client's data
// directory after resolution).
func (s *Service) RealPath(ctx context.Context, p string) (string, error) {
	if !path.IsAbs(p) {
		return "", &Error{Op: "resolve path", Err: errors.New("path must be absolute")}
	}
	var out string
	err := s.withClient(ctx, "resolve path", func(c *sftp.Client) error {
		var err error
		out, err = c.RealPath(p)
		return err
	})
	return out, err
}

// ReadRange reads up to limit bytes of a file from offset, opening it
// read-only. It returns fewer bytes at the end of the file.
func (s *Service) ReadRange(ctx context.Context, p string, offset int64, limit int) ([]byte, error) {
	if !path.IsAbs(p) || offset < 0 || limit <= 0 {
		return nil, &Error{Op: "read file", Err: errors.New("invalid read range")}
	}
	var out []byte
	err := s.withClient(ctx, "read file", func(c *sftp.Client) error {
		f, err := c.OpenFile(p, os.O_RDONLY)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return err
		}
		buf := make([]byte, limit)
		n, err := io.ReadFull(f, buf)
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			return err
		}
		out = buf[:n]
		return nil
	})
	return out, err
}
