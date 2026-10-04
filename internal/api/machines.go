package api

import (
	"context"
	"errors"
	"net/http"

	"hostbud/internal/inventory"
	"hostbud/internal/machines"
	"hostbud/internal/sshx"
)

// MachineRegistry is the runtime machine set (machines.Registry, V2-M13):
// the host and the servers added in the UI. When set it replaces
// Config.Machines and Config.FileSystem.
type MachineRegistry interface {
	Snapshotters() []Snapshotter
	FileSystemFor(machine string) (FileBrowser, bool)
	Scan(ctx context.Context, host string, port int) ([]sshx.HostKey, error)
	Add(ctx context.Context, s machines.Server) (inventory.Machine, error)
	Rename(ctx context.Context, id, label string) (inventory.Machine, error)
	Remove(ctx context.Context, id string) error
}

// machineList returns the tracked machines in display order.
func (s *server) machineList() []Snapshotter {
	if s.cfg.Registry != nil {
		return s.cfg.Registry.Snapshotters()
	}
	return s.cfg.Machines
}

// lookupMachine finds a tracked machine by id.
func (s *server) lookupMachine(id string) (Snapshotter, bool) {
	for _, m := range s.machineList() {
		if info, _ := m.Snapshot(); info.ID == id {
			return m, true
		}
	}
	return nil, false
}

// fileSystem returns the machine's file browser.
func (s *server) fileSystem(machine string) (FileBrowser, bool) {
	if s.cfg.Registry != nil {
		return s.cfg.Registry.FileSystemFor(machine)
	}
	return s.cfg.FileSystem, s.cfg.FileSystem != nil
}

type scanRequest struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type hostKeyInput struct {
	Type string `json:"type"`
	Key  string `json:"key"`
}

type addServerRequest struct {
	Label    string         `json:"label"`
	Host     string         `json:"host"`
	Port     int            `json:"port"`
	User     string         `json:"user"`
	HostKeys []hostKeyInput `json:"hostKeys"`
}

type renameServerRequest struct {
	Label string `json:"label"`
}

func (s *server) registry(w http.ResponseWriter) (MachineRegistry, bool) {
	if s.cfg.Registry == nil {
		writeError(w, http.StatusNotFound, "servers aren't available", "")
		return nil, false
	}
	return s.cfg.Registry, true
}

func (s *server) scanServer(w http.ResponseWriter, r *http.Request) {
	var req scanRequest
	if !decode(w, r, &req) {
		return
	}
	reg, ok := s.registry(w)
	if !ok {
		return
	}
	keys, err := reg.Scan(r.Context(), req.Host, req.Port)
	if err != nil {
		s.writeMachineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hostKeys": keys})
}

func (s *server) addServer(w http.ResponseWriter, r *http.Request) {
	var req addServerRequest
	if !decode(w, r, &req) {
		return
	}
	reg, ok := s.registry(w)
	if !ok {
		return
	}
	spec := machines.Server{Label: req.Label, Host: req.Host, Port: req.Port, User: req.User}
	for _, k := range req.HostKeys {
		spec.Keys = append(spec.Keys, sshx.HostKey{Type: k.Type, Key: k.Key})
	}
	m, err := reg.Add(r.Context(), spec)
	if err != nil {
		s.writeMachineError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

func (s *server) renameServer(w http.ResponseWriter, r *http.Request) {
	var req renameServerRequest
	if !decode(w, r, &req) {
		return
	}
	reg, ok := s.registry(w)
	if !ok {
		return
	}
	m, err := reg.Rename(r.Context(), r.PathValue("machine"), req.Label)
	if err != nil {
		s.writeMachineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *server) removeServer(w http.ResponseWriter, r *http.Request) {
	reg, ok := s.registry(w)
	if !ok {
		return
	}
	if err := reg.Remove(r.Context(), r.PathValue("machine")); err != nil {
		s.writeMachineError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

var machineCodeStatus = map[machines.Code]int{
	machines.CodeInvalid:  http.StatusBadRequest,
	machines.CodeNotFound: http.StatusNotFound,
	machines.CodeInUse:    http.StatusConflict,
	machines.CodeConflict: http.StatusConflict,
}

func (s *server) writeMachineError(w http.ResponseWriter, err error) {
	var me *machines.Error
	if errors.As(err, &me) {
		writeError(w, machineCodeStatus[me.Code], me.Message, me.Hint)
		return
	}
	var se *sshx.Error
	if errors.As(err, &se) {
		status := http.StatusBadGateway
		if se.Kind == sshx.KindTimeout {
			status = http.StatusGatewayTimeout
		}
		writeError(w, status, se.Message, se.Hint)
		return
	}
	s.cfg.Log.Error("server request failed", "err", err)
	writeError(w, http.StatusInternalServerError, "internal error", "")
}
