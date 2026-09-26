// Package sshx runs commands on machines through the system ssh binary with
// an app-generated config (docs/ARCHITECTURE.md §4). It is the only package
// that builds remote command lines.
package sshx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// HostMachineID is the machine id of the built-in host (see store).
const HostMachineID = "host"

// DefaultTimeout bounds every Exec whose context has no earlier deadline.
const DefaultTimeout = 10 * time.Second

// Config describes the ssh setup.
type Config struct {
	Dir         string // generated config, known_hosts and ControlMaster sockets (e.g. /data/ssh)
	HostKeysDir string // the host's public host keys (e.g. /run/host-keys)
	HostAddr    string // HostName of the host entry
	HostPort    int    // 0 = ssh's default
	HostUser    string // User of the host entry
	SSHBinary   string // default "ssh"
	Timeout     time.Duration
}

// Client runs remote commands.
type Client struct {
	cfg        Config
	configPath string
	agent      func() agentState
}

// New writes the ssh config and known_hosts (pinning the keys found in
// HostKeysDir) and returns a Client. Missing host keys are an error with
// instructions.
func New(cfg Config) (*Client, error) {
	if cfg.SSHBinary == "" {
		cfg.SSHBinary = "ssh"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultTimeout
	}
	path, err := writeFiles(cfg)
	if err != nil {
		return nil, err
	}
	return &Client{cfg: cfg, configPath: path, agent: checkAgent}, nil
}

// ConfigPath is the generated ssh config (for `ssh -F`).
func (c *Client) ConfigPath() string { return c.configPath }

// Alias returns the ssh config alias of a machine.
func Alias(machine string) (string, error) {
	if machine == HostMachineID {
		return HostAlias, nil
	}
	return "", fmt.Errorf("unknown machine %q", machine)
}

// Args returns the ssh argv (without the binary) that runs args on machine;
// extra ssh options (e.g. "-tt") go before the alias.
func (c *Client) Args(machine string, sshOpts []string, args ...string) ([]string, error) {
	alias, err := Alias(machine)
	if err != nil {
		return nil, err
	}
	argv := append([]string{"-F", c.configPath}, sshOpts...)
	return append(argv, alias, "--", Command(args...)), nil
}

// Binary is the ssh executable.
func (c *Client) Binary() string { return c.cfg.SSHBinary }

// OpenSFTP starts the system ssh binary's SFTP subsystem for machine. It uses
// the generated config and the same ControlMaster as Exec, but never builds a
// remote shell command. The returned stream owns the child process and should
// be closed when the SFTP client is closed.
func (c *Client) OpenSFTP(ctx context.Context, machine string) (io.ReadWriteCloser, error) {
	alias, err := Alias(machine)
	if err != nil {
		return nil, err
	}
	childCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(childCtx, c.cfg.SSHBinary, "-F", c.configPath, alias, "-s", "sftp") //nolint:gosec // fixed subsystem and generated alias
	cmd.WaitDelay = time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("open ssh stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("open ssh stdout: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start ssh SFTP subsystem: %w", err)
	}
	p := &subsystemPipe{stdin: stdin, stdout: stdout, cancel: cancel, done: make(chan struct{})}
	go func() {
		p.waitErr = cmd.Wait()
		close(p.done)
	}()
	return p, nil
}

type subsystemPipe struct {
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	cancel    context.CancelFunc
	done      chan struct{}
	waitErr   error
	closeOnce sync.Once
}

func (p *subsystemPipe) Read(b []byte) (int, error)  { return p.stdout.Read(b) }
func (p *subsystemPipe) Write(b []byte) (int, error) { return p.stdin.Write(b) }
func (p *subsystemPipe) Close() error {
	var err error
	p.closeOnce.Do(func() {
		_ = p.stdin.Close()
		p.cancel()
		_ = p.stdout.Close()
		<-p.done
		err = p.waitErr
	})
	return err
}

// Exec runs args (each shell-quoted) on machine and returns stdout. It is
// bounded by DefaultTimeout unless ctx has an earlier deadline; cancelling
// ctx kills ssh. Failures are *Error values with an actionable message.
func (c *Client) Exec(ctx context.Context, machine string, args ...string) ([]byte, error) {
	argv, err := c.Args(machine, nil, args...)
	if err != nil {
		return nil, err
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.cfg.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, c.cfg.SSHBinary, argv...) //nolint:gosec // argv is built from quoted args
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = time.Second // don't hang on pipes held open by children
	err = cmd.Run()
	if err == nil {
		return stdout.Bytes(), nil
	}

	if ctx.Err() != nil {
		return nil, &Error{
			Kind:    KindTimeout,
			Message: "the host didn't answer in time",
			Hint:    "The host may be overloaded or unreachable; hostbud retries automatically.",
			Stderr:  stderr.String(),
		}
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return nil, &Error{Kind: KindSSH, Message: "can't run ssh in the container: " + err.Error()}
	}
	if code := exitErr.ExitCode(); code != 255 {
		return stdout.Bytes(), &Error{Kind: KindRemote, ExitCode: code, Stderr: stderr.String()}
	}
	return nil, classify(stderr.String(), c.agent)
}

// checkAgent inspects SSH_AUTH_SOCK: missing socket, or no identities.
func checkAgent() agentState {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return agentMissing
	}
	st, err := os.Stat(sock) //nolint:gosec // SSH_AUTH_SOCK is operator config
	if err != nil || st.Mode()&os.ModeSocket == 0 {
		return agentMissing
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ssh-add", "-l").CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "no identities") {
			return agentEmpty
		}
		return agentMissing // e.g. "Could not open a connection to your authentication agent"
	}
	return agentOK
}

// Close stops the host's ControlMaster, if one is running.
func (c *Client) Close(ctx context.Context) error {
	argv := []string{"-F", c.configPath, "-O", "exit", HostAlias}
	cmd := exec.CommandContext(ctx, c.cfg.SSHBinary, argv...) //nolint:gosec // fixed argv
	cmd.WaitDelay = time.Second
	if out, err := cmd.CombinedOutput(); err != nil && !strings.Contains(string(out), "No such file") &&
		!strings.Contains(string(out), "Control socket connect") {
		return fmt.Errorf("stop ssh ControlMaster: %s", strings.TrimSpace(string(out)))
	}
	return nil
}
