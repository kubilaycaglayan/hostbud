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
	"syscall"
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
	// KeyscanBinary scans a new server's host keys (default "ssh-keyscan").
	KeyscanBinary string
	Timeout       time.Duration
}

// Client runs remote commands.
type Client struct {
	cfg        Config
	configPath string
	agent      func(context.Context) agentState
	masterOps  masterOperations
	timeoutMu  sync.Mutex
	timeouts   map[string]int
	recovering map[string]chan struct{}
	long       longLived

	// targetsMu guards the servers (V2-M13) and serializes rewrites of the
	// generated files.
	targetsMu sync.RWMutex
	targets   map[string]Target
	hostKnown string // known_hosts lines pinning the host's mounted keys
}

// New writes the ssh config and known_hosts (pinning the keys found in
// HostKeysDir) and returns a Client. Missing host keys are an error with
// instructions.
func New(cfg Config) (*Client, error) {
	if cfg.SSHBinary == "" {
		cfg.SSHBinary = "ssh"
	}
	if cfg.KeyscanBinary == "" {
		cfg.KeyscanBinary = "ssh-keyscan"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultTimeout
	}
	hostKnown, err := knownHosts(cfg.HostKeysDir)
	if err != nil {
		return nil, err
	}
	path, err := writeFiles(cfg, hostKnown, nil)
	if err != nil {
		return nil, err
	}
	c := &Client{cfg: cfg, configPath: path, agent: checkAgent, timeouts: map[string]int{}, recovering: map[string]chan struct{}{},
		targets: map[string]Target{}, hostKnown: hostKnown}
	c.masterOps = commandMasterOperations{client: c}
	return c, nil
}

// ConfigPath is the generated ssh config (for `ssh -F`).
func (c *Client) ConfigPath() string { return c.configPath }

// Alias returns the ssh config alias of a machine: the host or a server.
func (c *Client) Alias(machine string) (string, error) {
	if machine == HostMachineID {
		return HostAlias, nil
	}
	c.targetsMu.RLock()
	t, ok := c.targets[machine]
	c.targetsMu.RUnlock()
	if ok {
		return t.Alias, nil
	}
	return "", fmt.Errorf("unknown machine %q", machine)
}

// Args returns the ssh argv (without the binary) that runs args on machine;
// extra ssh options (e.g. "-tt") go before the alias.
func (c *Client) Args(machine string, sshOpts []string, args ...string) ([]string, error) {
	alias, err := c.Alias(machine)
	if err != nil {
		return nil, err
	}
	argv := append([]string{"-F", c.configPath}, sshOpts...)
	return append(argv, alias, "--", Command(args...)), nil
}

// Binary is the ssh executable.
func (c *Client) Binary() string { return c.cfg.SSHBinary }

// Timeout is the default deadline used for non-interactive remote commands.
func (c *Client) Timeout() time.Duration { return c.cfg.Timeout }

// OpenSFTP starts the system ssh binary's SFTP subsystem for machine. It uses
// the generated config and a long-lived ControlMaster (LongLived), and never
// builds a remote shell command. The returned stream owns the child process and should
// be closed when the SFTP client is closed.
func (c *Client) OpenSFTP(ctx context.Context, machine string) (io.ReadWriteCloser, error) {
	alias, err := c.Alias(machine)
	if err != nil {
		return nil, err
	}
	opts, release := c.LongLived(machine)
	childCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(childCtx, c.cfg.SSHBinary, append(append([]string{"-F", c.configPath}, opts...), alias, "-s", "sftp")...) //nolint:gosec // fixed subsystem and generated alias
	cmd.WaitDelay = time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		release()
		return nil, fmt.Errorf("open ssh stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		release()
		return nil, fmt.Errorf("open ssh stdout: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		cancel()
		release()
		return nil, fmt.Errorf("start ssh SFTP subsystem: %w", err)
	}
	p := &subsystemPipe{stdin: stdin, stdout: stdout, cancel: cancel, done: make(chan struct{})}
	go func() {
		p.waitErr = cmd.Wait()
		release()
		close(p.done)
	}()
	return p, nil
}

// Stream runs args (each shell-quoted) on machine with its stdin and stdout
// as a two-way stream, for a remote program that speaks a protocol (V2-M1:
// Codex's app-server proxy). Cancelling ctx or closing the stream ends it;
// the caller bounds it with a deadline.
func (c *Client) Stream(ctx context.Context, machine string, args ...string) (io.ReadWriteCloser, error) {
	opts, release := c.LongLived(machine)
	argv, err := c.Args(machine, opts, args...)
	if err != nil {
		release()
		return nil, err
	}
	childCtx, cancel := context.WithCancel(ctx)
	cmd := commandContext(childCtx, c.cfg.SSHBinary, argv...)
	cmd.WaitDelay = time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		release()
		return nil, fmt.Errorf("open ssh stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		release()
		return nil, fmt.Errorf("open ssh stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		release()
		return nil, fmt.Errorf("start ssh: %w", err)
	}
	p := &subsystemPipe{stdin: stdin, stdout: stdout, cancel: cancel, done: make(chan struct{})}
	go func() {
		p.waitErr = cmd.Wait()
		release()
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
func (c *Client) Exec(ctx context.Context, machine string, args ...string) (out []byte, resultErr error) {
	return c.exec(ctx, machine, nil, args...)
}

// ExecInput is Exec with stdin: the remote command reads input, which never
// appears in any process's argv (V2-M1: run tokens reach tmux this way).
func (c *Client) ExecInput(ctx context.Context, machine string, input []byte, args ...string) ([]byte, error) {
	return c.exec(ctx, machine, input, args...)
}

func (c *Client) exec(ctx context.Context, machine string, input []byte, args ...string) (out []byte, resultErr error) {
	argv, err := c.Args(machine, nil, args...)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	defer func() { c.recordExecResult(machine, resultErr, ctx) }()
	if err := c.awaitRecovery(ctx, machine); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, c.timeoutError("")
		}
		return nil, err
	}
	cmd := commandContext(ctx, c.cfg.SSHBinary, argv...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	cmd.WaitDelay = time.Second // don't hang on pipes held open by children
	err = cmd.Run()
	if err == nil {
		return stdout.Bytes(), nil
	}

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, c.timeoutError(stderr.String())
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return nil, &Error{Kind: KindSSH, Message: "can't run ssh in the container: " + err.Error()}
	}
	if code := exitErr.ExitCode(); code != 255 {
		return stdout.Bytes(), &Error{Kind: KindRemote, ExitCode: code, Stderr: stderr.String()}
	}
	return nil, forMachine(machine, classify(ctx, stderr.String(), c.agent))
}

// ExecTo runs args (each shell-quoted) on machine and streams its stdout to
// w (V2-M4 verify commands: w keeps only a bounded tail). Unlike Exec it
// has no DefaultTimeout: the caller bounds it with ctx, and ctx's error is
// returned when it ends the command. A non-zero exit is a KindRemote
// *Error; ssh failures are classified like Exec's.
func (c *Client) ExecTo(ctx context.Context, machine string, w io.Writer, args ...string) error {
	argv, err := c.Args(machine, nil, args...)
	if err != nil {
		return err
	}
	if err := c.awaitRecovery(ctx, machine); err != nil {
		return err
	}
	cmd := commandContext(ctx, c.cfg.SSHBinary, argv...)
	stderr := &capped{max: 64 << 10}
	cmd.Stdout, cmd.Stderr = w, stderr
	cmd.WaitDelay = time.Second
	err = cmd.Run()
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return &Error{Kind: KindSSH, Message: "can't run ssh in the container: " + err.Error()}
	}
	if code := exitErr.ExitCode(); code != 255 {
		return &Error{Kind: KindRemote, ExitCode: code, Stderr: stderr.String()}
	}
	return forMachine(machine, classify(ctx, stderr.String(), c.agent))
}

// capped keeps the first max bytes written to it.
type capped struct {
	buf bytes.Buffer
	max int
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.max - c.buf.Len(); room > 0 {
		c.buf.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

func (c *capped) String() string { return c.buf.String() }

func (c *Client) timeoutError(stderr string) *Error {
	return &Error{
		Kind: KindTimeout, Timeout: c.cfg.Timeout,
		Message: fmt.Sprintf("The host didn't answer within %s", c.cfg.Timeout),
		Hint:    "It may be overloaded or tmux may be stuck. hostbud will retry; check the host with `ssh <host> tmux ls`.",
		Stderr:  stderr,
	}
}

// commandContext starts a command in its own process group so cancellation
// kills ssh and any helper process it started, not only the group leader.
func commandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // caller supplies fixed or quoted argv
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	return cmd
}

// checkAgent inspects SSH_AUTH_SOCK: missing socket, or no identities.
func checkAgent(parent context.Context) agentState {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return agentMissing
	}
	st, err := os.Stat(sock) //nolint:gosec // SSH_AUTH_SOCK is operator config
	if err != nil || st.Mode()&os.ModeSocket == 0 {
		return agentMissing
	}
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
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

// Close stops the command ControlMasters of the host and every server.
func (c *Client) Close(ctx context.Context) error {
	aliases := []string{HostAlias}
	c.targetsMu.RLock()
	for _, t := range c.targets {
		aliases = append(aliases, t.Alias)
	}
	c.targetsMu.RUnlock()
	var errs []error
	for _, alias := range aliases {
		errs = append(errs, c.exitMaster(ctx, alias))
	}
	return errors.Join(errs...)
}

// exitMaster stops alias's master (sshOpts may select a long-lived one's
// ControlPath); a master that isn't running is not an error.
func (c *Client) exitMaster(ctx context.Context, alias string, sshOpts ...string) error {
	argv := append(append([]string{"-F", c.configPath}, sshOpts...), "-O", "exit", alias)
	cmd := exec.CommandContext(ctx, c.cfg.SSHBinary, argv...) //nolint:gosec // fixed argv; alias from the generated config
	cmd.WaitDelay = time.Second
	if out, err := cmd.CombinedOutput(); err != nil && !strings.Contains(string(out), "No such file") &&
		!strings.Contains(string(out), "Control socket connect") {
		return fmt.Errorf("stop ssh ControlMaster: %s", strings.TrimSpace(string(out)))
	}
	return nil
}
