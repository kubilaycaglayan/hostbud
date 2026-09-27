package sshx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const controlTimeout = 3 * time.Second

var masterPID = regexp.MustCompile(`pid=([0-9]+)`)

type masterOperations interface {
	exit(context.Context) error
	check(context.Context) ([]byte, error)
	kill(int) error
	sockets() ([]string, error)
	remove(string) error
}

type commandMasterOperations struct{ client *Client }

func (m commandMasterOperations) command(ctx context.Context, op string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, m.client.cfg.SSHBinary, "-F", m.client.configPath, "-O", op, HostAlias) //nolint:gosec // fixed ssh control argv
	cmd.WaitDelay = time.Second
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return output, ctx.Err()
	}
	return output, err
}

func (m commandMasterOperations) exit(ctx context.Context) error {
	_, err := m.command(ctx, "exit")
	return err
}

func (m commandMasterOperations) check(ctx context.Context) ([]byte, error) {
	return m.command(ctx, "check")
}

func (commandMasterOperations) kill(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}

func (m commandMasterOperations) sockets() ([]string, error) {
	dir := filepath.Join(m.client.cfg.Dir, "cm")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			paths = append(paths, filepath.Join(dir, entry.Name()))
		}
	}
	return paths, nil
}

func (commandMasterOperations) remove(path string) error { return os.Remove(path) }

func (c *Client) awaitRecovery(ctx context.Context, machine string) error {
	c.timeoutMu.Lock()
	done := c.recovering[machine]
	c.timeoutMu.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Client) recordExecResult(machine string, err error, ctx context.Context) {
	c.timeoutMu.Lock()
	if !IsKind(err, KindTimeout) {
		c.timeouts[machine] = 0
		c.timeoutMu.Unlock()
		return
	}
	c.timeouts[machine]++
	if c.timeouts[machine] < 2 || c.recovering[machine] != nil {
		c.timeoutMu.Unlock()
		return
	}
	done := make(chan struct{})
	c.recovering[machine] = done
	c.timeoutMu.Unlock()

	go func() {
		defer close(done)
		if err := c.resetMaster(ctx); err != nil {
			slog.Default().Warn("ssh ControlMaster recovery failed")
		} else {
			slog.Default().Warn("ssh ControlMaster reset after repeated timeouts")
		}
		c.timeoutMu.Lock()
		if c.recovering[machine] == done {
			delete(c.recovering, machine)
		}
		c.timeoutMu.Unlock()
	}()
}

func (c *Client) resetMaster(parent context.Context) error {
	base := context.WithoutCancel(parent)
	exitCtx, cancel := context.WithTimeout(base, controlTimeout)
	exitErr := c.masterOps.exit(exitCtx)
	cancel()

	checkCtx, cancel := context.WithTimeout(base, controlTimeout)
	output, checkErr := c.masterOps.check(checkCtx)
	cancel()
	if checkErr != nil {
		pid, err := c.findMasterProcess()
		if err != nil {
			if exitErr == nil { // Exit succeeded and no matching master remains.
				return c.removeSockets()
			}
			return fmt.Errorf("find unresponsive ssh ControlMaster: %w", err)
		}
		return c.killAndRemove(pid)
	}
	match := masterPID.FindSubmatch(output)
	if len(match) != 2 {
		return errors.New("ssh ControlMaster check did not include its pid")
	}
	pid, err := strconv.Atoi(string(match[1]))
	if err != nil || pid <= 1 {
		return errors.New("ssh ControlMaster check returned an invalid pid")
	}
	// An unresponsive master can leave an exit request queued on its socket.
	// Kill the checked pid even when -O exit reported success, then remove only
	// this client's sockets so the next Exec must establish a fresh master.
	return c.killAndRemove(pid)
}

func (c *Client) killAndRemove(pid int) error {
	if err := c.masterOps.kill(pid); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("kill ssh ControlMaster: %w", err)
	}

	return c.removeSockets()
}

func (c *Client) removeSockets() error {
	root, err := filepath.Abs(filepath.Join(c.cfg.Dir, "cm"))
	if err != nil {
		return err
	}
	sockets, err := c.masterOps.sockets()
	if err != nil {
		return fmt.Errorf("list ssh ControlMaster sockets: %w", err)
	}
	for _, socket := range sockets {
		abs, err := filepath.Abs(socket)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, abs)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("refusing to remove a socket outside hostbud's ControlMaster directory")
		}
		if err := c.masterOps.remove(abs); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove hostbud ControlMaster socket: %w", err)
		}
	}
	return nil
}

// findMasterProcess is the bounded-check fallback for a stopped master: the
// process argv must name both this generated config and our fixed alias.
func (c *Client) findMasterProcess() (int, error) {
	sockets, err := c.masterOps.sockets()
	if err != nil {
		return 0, err
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, err
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 1 {
			continue
		}
		argv, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if err != nil {
			continue
		}
		parts := strings.Split(string(argv), "\x00")
		config, alias, socket := false, false, false
		for i, part := range parts {
			if part == "-F" && i+1 < len(parts) && parts[i+1] == c.configPath {
				config = true
			}
			if part == HostAlias {
				alias = true
			}
			for _, path := range sockets {
				if path != "" && strings.Contains(part, path) {
					socket = true
				}
			}
		}
		if (config && alias) || socket {
			return pid, nil
		}
	}
	return 0, errors.New("no process matched the generated ssh config")
}
