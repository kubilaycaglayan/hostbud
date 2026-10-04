package sshx

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Target is a server added in the UI (V2-M13): a custom machine reached
// through its own Host block in the generated config. Its keys are the ones
// the owner confirmed; they are pinned under Alias (HostKeyAlias).
type Target struct {
	ID       string
	Alias    string
	HostName string
	Port     int
	User     string
	Keys     []HostKey
}

// HostKey is one public host key. Fingerprint is OpenSSH's SHA256 form.
type HostKey struct {
	Type        string `json:"type"`
	Key         string `json:"key"`
	Fingerprint string `json:"fingerprint"`
}

// TargetAliasPrefix starts every server alias: hostbud-custom-<id>.
const TargetAliasPrefix = "hostbud-custom-"

var (
	targetIDRE = regexp.MustCompile(`^s-[a-z0-9]{6,32}$`)
	hostNameRE = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)
	userRE     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,31}$`)
)

// ValidateHostName accepts a DNS name or an IP address (no options, spaces
// or leading dashes reach ssh or ssh-keyscan).
func ValidateHostName(h string) error {
	if _, err := netip.ParseAddr(h); err == nil && !strings.Contains(h, "%") {
		return nil
	}
	if !hostNameRE.MatchString(h) || strings.Contains(h, "..") {
		return errors.New("use a host name like server-a.example.com or an IP address")
	}
	return nil
}

// ValidateUser accepts a POSIX-style login name.
func ValidateUser(u string) error {
	if !userRE.MatchString(u) {
		return errors.New("use a login name like dev (letters, digits, '_', '.', '-')")
	}
	return nil
}

// ValidatePort accepts 1–65535.
func ValidatePort(p int) error {
	if p < 1 || p > 65535 {
		return errors.New("use a port from 1 to 65535")
	}
	return nil
}

// ParseHostKey checks a public key (type and base64 blob, as ssh-keyscan
// prints them) and returns it with its fingerprint.
func ParseHostKey(keyType, key string) (HostKey, error) {
	if !isKeyType(keyType) || len(keyType) > 64 || len(key) > 16<<10 {
		return HostKey{}, fmt.Errorf("%q is not a public key type", keyType)
	}
	blob, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(blob) < 4 {
		return HostKey{}, errors.New("the key is not valid base64")
	}
	n := binary.BigEndian.Uint32(blob)
	if uint64(len(blob)) < 4+uint64(n) || string(blob[4:4+n]) != keyType {
		return HostKey{}, errors.New("the key doesn't match its type")
	}
	sum := sha256.Sum256(blob)
	return HostKey{Type: keyType, Key: key, Fingerprint: "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])}, nil
}

// ValidateTarget checks every field that reaches the generated config.
func ValidateTarget(t Target) error {
	if !targetIDRE.MatchString(t.ID) || t.Alias != TargetAliasPrefix+t.ID {
		return fmt.Errorf("invalid server id %q", t.ID)
	}
	if err := ValidateHostName(t.HostName); err != nil {
		return err
	}
	if err := ValidateUser(t.User); err != nil {
		return err
	}
	if err := ValidatePort(t.Port); err != nil {
		return err
	}
	if len(t.Keys) == 0 {
		return errors.New("confirm at least one host key")
	}
	for _, k := range t.Keys {
		if _, err := ParseHostKey(k.Type, k.Key); err != nil {
			return err
		}
	}
	return nil
}

// SetTargets replaces the servers and rewrites the generated config and
// known_hosts. Every target is validated first; on error nothing changes.
func (c *Client) SetTargets(targets []Target) error {
	for _, t := range targets {
		if err := ValidateTarget(t); err != nil {
			return err
		}
	}
	sorted := append([]Target(nil), targets...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	c.targetsMu.Lock()
	defer c.targetsMu.Unlock()
	if _, err := writeFiles(c.cfg, c.hostKnown, sorted); err != nil {
		return err
	}
	c.targets = make(map[string]Target, len(sorted))
	for _, t := range sorted {
		c.targets[t.ID] = t
	}
	return nil
}

// Targets returns the current servers, sorted by id.
func (c *Client) Targets() []Target {
	c.targetsMu.RLock()
	defer c.targetsMu.RUnlock()
	out := make([]Target, 0, len(c.targets))
	for _, t := range c.targets {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// CloseMachine stops a server's command master and its long-lived masters,
// before it is removed from the config. It never touches the remote tmux.
func (c *Client) CloseMachine(ctx context.Context, machine string) error {
	alias, err := c.Alias(machine)
	if err != nil {
		return nil
	}
	c.long.mu.Lock()
	shards := len(c.long.shards[machine])
	delete(c.long.shards, machine)
	c.long.mu.Unlock()
	errs := []error{c.exitMaster(ctx, alias)}
	for i := range shards {
		path := c.cfg.Dir + "/" + longLivedDir + "/%C-" + strconv.Itoa(i)
		errs = append(errs, c.exitMaster(ctx, alias, "-o", "ControlPath="+path))
	}
	return errors.Join(errs...)
}

// scanTimeout bounds ssh-keyscan (its own -T is per connection attempt).
const scanTimeout = 10 * time.Second

// Scan fetches a server's public host keys for the owner to confirm. The
// keys are never trusted here: the caller pins only what the owner chose.
func (c *Client) Scan(ctx context.Context, host string, port int) ([]HostKey, error) {
	if err := ValidateHostName(host); err != nil {
		return nil, err
	}
	if err := ValidatePort(port); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, scanTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.cfg.KeyscanBinary, "-T", "5", "-p", strconv.Itoa(port), host) //nolint:gosec // validated host and port
	cmd.WaitDelay = time.Second
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	runErr := cmd.Run()
	keys := parseKeyscan(stdout.Bytes())
	if len(keys) > 0 {
		return keys, nil
	}
	if ctx.Err() != nil {
		return nil, &Error{Kind: KindTimeout, Message: "the server didn't answer the host-key scan in time",
			Hint: "Check the host name and port, and that hostbud's container can reach the server."}
	}
	var exitErr *exec.ExitError
	if runErr != nil && !errors.As(runErr, &exitErr) {
		return nil, &Error{Kind: KindSSH, Message: "can't run ssh-keyscan in the container: " + runErr.Error()}
	}
	return nil, &Error{Kind: KindUnreachable, Message: "the server sent no SSH host keys",
		Hint: "Check the host name and port, and that sshd is running on the server."}
}

// parseKeyscan reads "host type key" lines, skipping comments, malformed or
// repeated keys.
func parseKeyscan(out []byte) []HostKey {
	var keys []HostKey
	seen := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64<<10), 64<<10)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 3 || strings.HasPrefix(f[0], "#") {
			continue
		}
		k, err := ParseHostKey(f[1], f[2])
		if err != nil || seen[k.Key] {
			continue
		}
		seen[k.Key] = true
		keys = append(keys, k)
	}
	return keys
}

// forMachine rewrites the host-centric messages and hints of classify for a
// server added in the UI.
func forMachine(machine string, e *Error) *Error {
	if machine == HostMachineID || e == nil {
		return e
	}
	switch {
	case e.Kind == KindHostKey:
		e.Message = "The server's SSH key doesn't match the key you confirmed."
		e.Hint = "If the server was reinstalled, remove it under Add server and add it again to confirm its new key; otherwise don't connect."
	case e.Kind == KindUnreachable && strings.Contains(e.Message, "resolve"):
		e.Message = "can't resolve the server's address"
		e.Hint = "Check the server's host name; hostbud resolves it from inside its container."
	case e.Kind == KindUnreachable:
		e.Message = strings.Replace(e.Message, "on the host", "on the server", 1)
		e.Hint = "Check the server's host name and port, that sshd runs there, and that hostbud's container can reach it."
	case e.Kind == KindAuth:
		e.Message = "the server refused hostbud's SSH key (permission denied)"
		e.Hint = "Add the public key of the key in hostbud's agent (e.g. ~/.ssh/hostbud_ed25519.pub) to ~/.ssh/authorized_keys of that user on the server."
	case e.Kind == KindSSH:
		e.Message = "ssh to the server failed"
	}
	return e
}
