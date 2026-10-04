package store

import (
	"context"
	"errors"
	"testing"
)

func TestCustomMachineLifecycle(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t, t.TempDir())
	defer func() { _ = s.Close() }()
	if _, err := s.EnsureHostMachine(ctx, "Host machine"); err != nil {
		t.Fatal(err)
	}
	host, _ := s.Machine(ctx, HostMachineID)
	if host.Port != 22 || host.HostName != "" || host.HostKeys != "" {
		t.Fatalf("host connection defaults = %+v", host)
	}

	in := NewMachine{ID: "s-0a1b2c3d4e", SSHAlias: "hostbud-custom-s-0a1b2c3d4e", Label: "Build box",
		HostName: "server-a.example.com", Port: 2222, SSHUser: "dev", HostKeys: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl\n"}
	m, err := s.CreateMachine(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if m.Source != "custom" || !m.Active || m.Label != "Build box" || m.HostName != in.HostName || m.Port != 2222 ||
		m.SSHUser != "dev" || m.HostKeys != in.HostKeys || m.SSHAlias != in.SSHAlias || m.SortOrder <= host.SortOrder {
		t.Fatalf("created = %+v", m)
	}
	if _, err := s.CreateMachine(ctx, in); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate id: %v", err)
	}
	all, err := s.Machines(ctx)
	if err != nil || len(all) != 2 || all[0].ID != HostMachineID || all[1].ID != in.ID {
		t.Fatalf("order = %+v, %v", all, err)
	}

	renamed, err := s.RenameMachine(ctx, in.ID, "CI box")
	if err != nil || renamed.Label != "CI box" {
		t.Fatalf("rename = %+v, %v", renamed, err)
	}
	if _, err := s.RenameMachine(ctx, HostMachineID, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("host rename: %v", err)
	}

	// Projects keep a server; session links and the run cap go with it.
	p, err := s.CreateProject(ctx, in.ID, "/home/dev/app", "App")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertSessionLink(ctx, in.ID, "work", p.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteMachine(ctx, in.ID); !errors.Is(err, ErrInUse) {
		t.Fatalf("delete with project: %v", err)
	}
	if err := s.DeleteProject(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteMachine(ctx, in.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Machine(ctx, in.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
	if err := s.DeleteMachine(ctx, in.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
	if err := s.DeleteMachine(ctx, HostMachineID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("host delete: %v", err)
	}
}
