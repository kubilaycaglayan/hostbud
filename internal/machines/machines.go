// Package machines is the runtime registry of tracked machines: the
// built-in host and the servers added in the UI (V2-M13). It owns one
// inventory poller and one SFTP browser per machine, keeps sshx's generated
// config in step with the stored servers, and starts or stops tracking as
// servers are added or removed. Removing a server never touches its tmux.
package machines

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"hostbud/internal/events"
	"hostbud/internal/fsbrowse"
	"hostbud/internal/inventory"
	"hostbud/internal/sshx"
	"hostbud/internal/store"
)

// MaxLabel is the longest nickname, in characters.
const MaxLabel = 40

// maxKeys bounds the host keys pinned for one server.
const maxKeys = 10

// Code classifies registry errors for the API.
type Code string

const (
	CodeInvalid  Code = "invalid"
	CodeNotFound Code = "not_found"
	CodeInUse    Code = "in_use"
	CodeConflict Code = "conflict"
)

// Error is an actionable, user-facing error.
type Error struct {
	Code    Code
	Message string
	Hint    string
}

func (e *Error) Error() string { return e.Message }

func invalid(msg, hint string) *Error { return &Error{Code: CodeInvalid, Message: msg, Hint: hint} }

// Store is the persistence the registry needs (store.Store).
type Store interface {
	Machines(ctx context.Context) ([]store.Machine, error)
	CreateMachine(ctx context.Context, m store.NewMachine) (store.Machine, error)
	RenameMachine(ctx context.Context, id, label string) (store.Machine, error)
	UpdateServer(ctx context.Context, id string, m store.NewMachine) (store.Machine, error)
	DeleteMachine(ctx context.Context, id string) error
}

// SSH is what the registry needs from sshx.Client.
type SSH interface {
	inventory.Executor
	fsbrowse.SubsystemOpener
	SetTargets(targets []sshx.Target) error
	Targets() []sshx.Target
	CloseMachine(ctx context.Context, machine string) error
	Scan(ctx context.Context, host string, port int) ([]sshx.HostKey, error)
}

// Options configure the registry.
type Options struct {
	HostLabel    string
	PollInterval time.Duration
	Capabilities inventory.CapabilityStore
	// SFTP timeouts for each machine's file browser.
	SFTPIdle, SFTPTimeout, UploadTimeout time.Duration
	Log                                  *slog.Logger
}

// Server is a server to add, with the host keys the owner confirmed.
type Server struct {
	Label string
	Host  string
	Port  int
	User  string
	Keys  []sshx.HostKey // type and key; fingerprints are recomputed
}

type entry struct {
	inv    *inventory.Inventory
	fs     *fsbrowse.Service
	cancel context.CancelFunc
	done   chan struct{}
}

// Registry tracks machines. Safe for concurrent use.
type Registry struct {
	ssh   SSH
	store Store
	bus   *events.Bus
	opt   Options

	mu      sync.RWMutex
	entries map[string]*entry
	order   []string
	ctx     context.Context // set by Start; nil before
	wg      sync.WaitGroup
}

// New returns an empty registry; call Load, then Start.
func New(ssh SSH, st Store, bus *events.Bus, opt Options) *Registry {
	if opt.Log == nil {
		opt.Log = slog.New(slog.DiscardHandler)
	}
	if opt.SFTPIdle <= 0 {
		opt.SFTPIdle = fsbrowse.DefaultIdleTimeout
	}
	return &Registry{ssh: ssh, store: st, bus: bus, opt: opt, entries: map[string]*entry{}}
}

// Load registers the host and every stored server and writes the servers
// into the ssh config. A stored server that no longer validates is skipped
// with a warning, so one bad row can't keep hostbud from starting.
func (r *Registry) Load(ctx context.Context) error {
	rows, err := r.store.Machines(ctx)
	if err != nil {
		return fmt.Errorf("load machines: %w", err)
	}
	var targets []sshx.Target
	var servers []store.Machine
	for _, m := range rows {
		if m.Source != "custom" {
			continue
		}
		t, err := targetOf(m)
		if err == nil {
			err = sshx.ValidateTarget(t)
		}
		if err != nil {
			r.opt.Log.Warn("skipping a stored server that doesn't validate", "machine", m.ID, "err", err)
			continue
		}
		targets = append(targets, t)
		servers = append(servers, m)
	}
	if err := r.ssh.SetTargets(targets); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.register(store.HostMachineID, r.opt.HostLabel, "host", "")
	for _, m := range servers {
		r.register(m.ID, m.Label, "custom", address(m.SSHUser, m.HostName, m.Port))
	}
	return nil
}

// Start starts polling every registered machine (and, from now on, every
// added one) until ctx ends.
func (r *Registry) Start(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ctx = ctx
	for _, id := range r.order {
		r.run(r.entries[id])
	}
}

// Wait blocks until every poller has stopped (after Start's ctx ended).
func (r *Registry) Wait() { r.wg.Wait() }

// Close closes every file browser.
func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var errs []error
	for _, e := range r.entries {
		errs = append(errs, e.fs.Close())
	}
	return errors.Join(errs...)
}

// register adds an entry; r.mu must be held.
func (r *Registry) register(id, label, source, addr string) *entry {
	e := &entry{
		inv: inventory.New(r.ssh, r.bus, inventory.Options{
			MachineID: id, Label: label, Source: source, Address: addr, Interval: r.opt.PollInterval,
			Store: r.opt.Capabilities, Log: r.opt.Log,
		}),
		fs:   fsbrowse.New(r.ssh, id, r.opt.SFTPIdle, r.opt.SFTPTimeout, r.opt.UploadTimeout),
		done: make(chan struct{}),
	}
	r.entries[id] = e
	r.order = append(r.order, id)
	return e
}

// run starts e's poller if the registry has started; r.mu must be held.
func (r *Registry) run(e *entry) {
	if r.ctx == nil || e.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(r.ctx)
	e.cancel = cancel
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer close(e.done)
		e.inv.Run(ctx)
	}()
}

// Inventory returns a machine's poller.
func (r *Registry) Inventory(id string) (*inventory.Inventory, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[id]
	if !ok {
		return nil, false
	}
	return e.inv, true
}

// FileSystem returns a machine's file browser.
func (r *Registry) FileSystem(id string) (*fsbrowse.Service, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[id]
	if !ok {
		return nil, false
	}
	return e.fs, true
}

// Inventories returns every poller in display order (host first).
func (r *Registry) Inventories() []*inventory.Inventory {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*inventory.Inventory, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, r.entries[id].inv)
	}
	return out
}

// Scan fetches a server's host keys for the owner to confirm.
func (r *Registry) Scan(ctx context.Context, host string, port int) ([]sshx.HostKey, error) {
	host = strings.TrimSpace(host)
	if err := sshx.ValidateHostName(host); err != nil {
		return nil, invalid("invalid host: "+err.Error(), "")
	}
	if port == 0 {
		port = 22
	}
	if err := sshx.ValidatePort(port); err != nil {
		return nil, invalid("invalid port: "+err.Error(), "")
	}
	return r.ssh.Scan(ctx, host, port)
}

// Add stores a server, pins its confirmed keys and starts tracking it.
func (r *Registry) Add(ctx context.Context, s Server) (inventory.Machine, error) {
	label, err := r.checkLabel(s.Label, "")
	if err != nil {
		return inventory.Machine{}, err
	}
	s.Host, s.User = strings.TrimSpace(s.Host), strings.TrimSpace(s.User)
	if s.Port == 0 {
		s.Port = 22
	}
	if err := sshx.ValidateHostName(s.Host); err != nil {
		return inventory.Machine{}, invalid("invalid host: "+err.Error(), "")
	}
	if err := sshx.ValidateUser(s.User); err != nil {
		return inventory.Machine{}, invalid("invalid user: "+err.Error(), "")
	}
	if err := sshx.ValidatePort(s.Port); err != nil {
		return inventory.Machine{}, invalid("invalid port: "+err.Error(), "")
	}
	if len(s.Keys) == 0 || len(s.Keys) > maxKeys {
		return inventory.Machine{}, invalid("confirm the server's host key", "Check the host key first, compare its fingerprint, then add the server.")
	}
	keys := make([]sshx.HostKey, 0, len(s.Keys))
	var lines strings.Builder
	for _, k := range s.Keys {
		parsed, err := sshx.ParseHostKey(k.Type, k.Key)
		if err != nil {
			return inventory.Machine{}, invalid("invalid host key: "+err.Error(), "")
		}
		keys = append(keys, parsed)
		lines.WriteString(parsed.Type + " " + parsed.Key + "\n")
	}
	id, err := newID()
	if err != nil {
		return inventory.Machine{}, err
	}
	target := sshx.Target{ID: id, Alias: sshx.TargetAliasPrefix + id, HostName: s.Host, Port: s.Port, User: s.User, Keys: keys}
	if err := sshx.ValidateTarget(target); err != nil {
		return inventory.Machine{}, invalid(err.Error(), "")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := r.store.CreateMachine(ctx, store.NewMachine{
		ID: id, SSHAlias: target.Alias, Label: label, HostName: s.Host, Port: s.Port, SSHUser: s.User, HostKeys: lines.String(),
	}); err != nil {
		return inventory.Machine{}, err
	}
	if err := r.ssh.SetTargets(append(r.ssh.Targets(), target)); err != nil {
		if delErr := r.store.DeleteMachine(context.WithoutCancel(ctx), id); delErr != nil {
			r.opt.Log.Warn("can't roll back a server that failed to configure", "machine", id, "err", delErr)
		}
		return inventory.Machine{}, err
	}
	e := r.register(id, label, "custom", address(s.User, s.Host, s.Port))
	m, _ := e.inv.Snapshot()
	r.bus.Publish(events.Event{Type: events.MachineStatus, Machine: id, Payload: m})
	r.run(e)
	return m, nil
}

// Rename changes a server's nickname.
func (r *Registry) Rename(ctx context.Context, id, label string) (inventory.Machine, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[id]
	if !ok || id == store.HostMachineID {
		return inventory.Machine{}, &Error{Code: CodeNotFound, Message: "no such server", Hint: "The host's label comes from HOSTBUD_HOST_LABEL."}
	}
	label, err := r.checkLabelLocked(label, id)
	if err != nil {
		return inventory.Machine{}, err
	}
	if _, err := r.store.RenameMachine(ctx, id, label); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return inventory.Machine{}, &Error{Code: CodeNotFound, Message: "no such server"}
		}
		return inventory.Machine{}, err
	}
	e.inv.SetLabel(label)
	m, _ := e.inv.Snapshot()
	return m, nil
}

// Update changes a server's nickname and SSH connection. The caller must supply
// host keys confirmed for the submitted host and port.
func (r *Registry) Update(ctx context.Context, id string, s Server) (inventory.Machine, error) {
	s.Label = strings.TrimSpace(s.Label)
	s.Host, s.User = strings.TrimSpace(s.Host), strings.TrimSpace(s.User)
	if err := sshx.ValidateHostName(s.Host); err != nil {
		return inventory.Machine{}, invalid("invalid host: "+err.Error(), "")
	}
	if err := sshx.ValidateUser(s.User); err != nil {
		return inventory.Machine{}, invalid("invalid user: "+err.Error(), "")
	}
	if err := sshx.ValidatePort(s.Port); err != nil {
		return inventory.Machine{}, invalid("invalid port: "+err.Error(), "")
	}
	if len(s.Keys) == 0 || len(s.Keys) > maxKeys {
		return inventory.Machine{}, invalid("confirm the server's host key", "Check the host key first and compare its fingerprint.")
	}
	keys := make([]sshx.HostKey, 0, len(s.Keys))
	var lines strings.Builder
	for _, k := range s.Keys {
		parsed, err := sshx.ParseHostKey(k.Type, k.Key)
		if err != nil {
			return inventory.Machine{}, invalid("invalid host key: "+err.Error(), "")
		}
		keys = append(keys, parsed)
		lines.WriteString(parsed.Type + " " + parsed.Key + "\n")
	}
	target := sshx.Target{ID: id, Alias: sshx.TargetAliasPrefix + id, HostName: s.Host, Port: s.Port, User: s.User, Keys: keys}
	if err := sshx.ValidateTarget(target); err != nil {
		return inventory.Machine{}, invalid(err.Error(), "")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := r.checkLabelLocked(s.Label, id); err != nil {
		return inventory.Machine{}, err
	}
	e, ok := r.entries[id]
	if !ok || id == store.HostMachineID {
		return inventory.Machine{}, &Error{Code: CodeNotFound, Message: "no such server"}
	}
	// Apply SSH config first; retain the stored/current runtime state on failure.
	targets := r.ssh.Targets()
	found := false
	for i := range targets {
		if targets[i].ID == id {
			targets[i] = target
			found = true
		}
	}
	if !found {
		targets = append(targets, target)
	}
	if err := r.ssh.SetTargets(targets); err != nil {
		return inventory.Machine{}, err
	}
	if _, err := r.store.UpdateServer(ctx, id, store.NewMachine{Label: s.Label, HostName: s.Host, Port: s.Port, SSHUser: s.User, HostKeys: lines.String()}); err != nil {
		return inventory.Machine{}, err
	}
	if e.cancel != nil {
		e.cancel()
		<-e.done
	}
	_ = e.fs.Close()
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	_ = r.ssh.CloseMachine(closeCtx, id)
	cancel()
	for i, v := range r.order {
		if v == id {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
	delete(r.entries, id)
	e = r.register(id, s.Label, "custom", address(s.User, s.Host, s.Port))
	r.run(e)
	m, _ := e.inv.Snapshot()
	r.bus.Publish(events.Event{Type: events.MachineStatus, Machine: id, Payload: m})
	return m, nil
}

// Remove stops tracking a server and forgets it. It is refused while
// projects use the server; the server's tmux sessions keep running.
func (r *Registry) Remove(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[id]
	if !ok || id == store.HostMachineID {
		return &Error{Code: CodeNotFound, Message: "no such server", Hint: "The host can't be removed."}
	}
	if err := r.store.DeleteMachine(ctx, id); err != nil {
		switch {
		case errors.Is(err, store.ErrInUse):
			return &Error{Code: CodeInUse, Message: "projects still use this server",
				Hint: "Remove the server's projects from the tree first; its tmux sessions keep running."}
		case errors.Is(err, store.ErrNotFound):
			return &Error{Code: CodeNotFound, Message: "no such server"}
		}
		return err
	}
	if e.cancel != nil {
		e.cancel()
		<-e.done
	}
	_ = e.fs.Close()
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	if err := r.ssh.CloseMachine(closeCtx, id); err != nil {
		r.opt.Log.Debug("stop a removed server's ssh masters", "machine", id, "err", err)
	}
	cancel()
	var keep []sshx.Target
	for _, t := range r.ssh.Targets() {
		if t.ID != id {
			keep = append(keep, t)
		}
	}
	if err := r.ssh.SetTargets(keep); err != nil {
		r.opt.Log.Warn("can't rewrite the ssh config after removing a server", "err", err)
	}
	delete(r.entries, id)
	for i, o := range r.order {
		if o == id {
			r.order = append(r.order[:i:i], r.order[i+1:]...)
			break
		}
	}
	r.bus.Publish(events.Event{Type: events.MachineRemoved, Machine: id, Payload: map[string]string{"id": id}})
	return nil
}

func (r *Registry) checkLabel(label, self string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.checkLabelLocked(label, self)
}

// checkLabelLocked validates a nickname: 1–40 characters, no control
// characters, unique among machines (case-insensitive). r.mu must be held.
func (r *Registry) checkLabelLocked(label, self string) (string, error) {
	label = strings.TrimSpace(label)
	if label == "" || utf8.RuneCountInString(label) > MaxLabel {
		return "", invalid("invalid nickname", "Use 1 to "+strconv.Itoa(MaxLabel)+" characters, e.g. build-box.")
	}
	for _, c := range label {
		if unicode.IsControl(c) {
			return "", invalid("invalid nickname", "Nicknames can't contain control characters.")
		}
	}
	for id, e := range r.entries {
		if id == self {
			continue
		}
		if m, _ := e.inv.Snapshot(); strings.EqualFold(m.Label, label) {
			return "", &Error{Code: CodeConflict, Message: "another server already has this nickname", Hint: "Pick a different nickname."}
		}
	}
	return label, nil
}

func targetOf(m store.Machine) (sshx.Target, error) {
	t := sshx.Target{ID: m.ID, Alias: m.SSHAlias, HostName: m.HostName, Port: m.Port, User: m.SSHUser}
	for _, line := range strings.Split(strings.TrimSpace(m.HostKeys), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		k, err := sshx.ParseHostKey(f[0], f[1])
		if err != nil {
			return t, err
		}
		t.Keys = append(t.Keys, k)
	}
	return t, nil
}

func address(user, host string, port int) string {
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return user + "@" + host + ":" + strconv.Itoa(port)
}

func newID() (string, error) {
	b := make([]byte, 5)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "s-" + hex.EncodeToString(b), nil
}
