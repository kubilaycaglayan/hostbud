// Package sshx runs commands on machines through the system ssh binary with
// an app-generated config (docs/ARCHITECTURE.md §4). It is the only package
// that builds remote command lines.
package sshx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
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
