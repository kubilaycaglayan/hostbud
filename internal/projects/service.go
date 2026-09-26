// Package projects owns project persistence coordination, live-session
// placement and project-related events.
package projects

import (
	"context"
	"errors"
	"log/slog"
	"path"
	"strings"

	"hostbud/internal/events"
	"hostbud/internal/inventory"
	"hostbud/internal/session"
	"hostbud/internal/store"
)

var ErrInvalidInput = errors.New("invalid project input")

const (
	maxPathBytes = 4096
	maxNameBytes = 255
)

// Repository is the persistence surface needed by the project service.
type Repository interface {
	Projects(context.Context, string) ([]store.Project, error)
	Project(context.Context, string) (store.Project, error)
	CreateProject(context.Context, string, string, string) (store.Project, error)
	RenameProject(context.Context, string, string) (store.Project, error)
	SessionLink(context.Context, string, string) (store.SessionLink, error)
	UpsertSessionLink(context.Context, string, string, string) error
	RenameSessionLink(context.Context, string, string, string) error
	DeleteSessionLink(context.Context, string, string) error
	PruneSessionLinks(context.Context, string, []string) error
}

// SessionCreator is the existing single entry point for tmux session creation.
type SessionCreator interface {
	Create(context.Context, session.Spec) (string, error)
}

// Changed is the payload of a projects.changed event.
type Changed struct {
	Action  string        `json:"action"`
	Project store.Project `json:"project"`
}

// Placement tells the UI which project owns a live session, when any.
type Placement struct {
	MachineID string `json:"machineId"`
	ProjectID string `json:"projectId,omitempty"`
	Matched   bool   `json:"matched"`
}

// Service coordinates project state, events and session associations.
type Service struct {
	repo    Repository
	bus     *events.Bus
	creator SessionCreator
	log     *slog.Logger
}

func New(repo Repository, bus *events.Bus, log *slog.Logger) *Service {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{repo: repo, bus: bus, log: log}
}

func (s *Service) SetSessionCreator(creator SessionCreator) { s.creator = creator }

func validateProject(machineID, rawPath, rawName string) (string, string, error) {
	if strings.TrimSpace(machineID) == "" || rawPath == "" || len(rawPath) > maxPathBytes ||
		strings.IndexByte(rawPath, 0) >= 0 || !path.IsAbs(rawPath) {
		return "", "", ErrInvalidInput
	}
	clean := path.Clean(rawPath)
	name := strings.TrimSpace(rawName)
	if name == "" && rawName == "" {
		name = path.Base(clean)
	}
	if name == "" || len(name) > maxNameBytes || strings.IndexByte(name, 0) >= 0 {
		return "", "", ErrInvalidInput
	}
	return clean, name, nil
}

func (s *Service) List(ctx context.Context, machineID string) ([]store.Project, error) {
	return s.repo.Projects(ctx, machineID)
}

func (s *Service) Get(ctx context.Context, id string) (store.Project, error) {
	return s.repo.Project(ctx, id)
}

func (s *Service) Create(ctx context.Context, machineID, projectPath, name string) (store.Project, error) {
	clean, name, err := validateProject(machineID, projectPath, name)
	if err != nil {
		return store.Project{}, err
	}
	existing, err := s.repo.Projects(ctx, machineID)
	if err != nil {
		return store.Project{}, err
	}
	for _, p := range existing {
		if p.Path == clean {
			return s.repo.CreateProject(ctx, machineID, clean, name)
		}
	}
	p, err := s.repo.CreateProject(ctx, machineID, clean, name)
	if err == nil {
		s.publish("upsert", p)
	}
	return p, err
}

func (s *Service) Rename(ctx context.Context, id, name string) (store.Project, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > maxNameBytes || strings.IndexByte(name, 0) >= 0 {
		return store.Project{}, ErrInvalidInput
	}
	p, err := s.repo.RenameProject(ctx, id, name)
	if err == nil {
		s.publish("upsert", p)
	}
	return p, err
}

func (s *Service) publish(action string, p store.Project) {
	if s.bus == nil {
		return
	}
	s.bus.Publish(events.Event{Type: events.ProjectsChanged, Machine: p.MachineID, Payload: Changed{Action: action, Project: p}})
}

// CreateSession creates a session through the shared session service, using
// the saved project's exact machine and path, then records its link hint.
func (s *Service) CreateSession(ctx context.Context, projectID string, spec session.Spec) (string, error) {
	if s.creator == nil {
		return "", errors.New("session service is unavailable")
	}
	p, err := s.repo.Project(ctx, projectID)
	if err != nil {
		return "", err
	}
	spec.Machine, spec.Path = p.MachineID, p.Path
	name, err := s.creator.Create(ctx, spec)
	if err != nil {
		return "", err
	}
	if err := s.repo.UpsertSessionLink(ctx, p.MachineID, name, p.ID); err != nil {
		// Path-prefix placement still works if metadata storage briefly fails.
		s.log.Error("save project session link", "machine", p.MachineID, "session", name, "err", err)
	}
	return name, nil
}

// Place resolves an explicit link first, then the longest path-component
// prefix on the same machine. Equal-length ties use stable project ID order.
func (s *Service) Place(ctx context.Context, machineID, sessionName, sessionPath string) (Placement, error) {
	result := Placement{MachineID: machineID}
	if link, err := s.repo.SessionLink(ctx, machineID, sessionName); err == nil {
		p, projectErr := s.repo.Project(ctx, link.ProjectID)
		if projectErr == nil && p.MachineID == machineID {
			result.ProjectID, result.Matched = p.ID, true
			return result, nil
		}
		if projectErr != nil && !errors.Is(projectErr, store.ErrNotFound) {
			return result, projectErr
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return result, err
	}
	if !path.IsAbs(sessionPath) || strings.IndexByte(sessionPath, 0) >= 0 {
		return result, nil
	}
	sessionPath = path.Clean(sessionPath)
	projects, err := s.repo.Projects(ctx, machineID)
	if err != nil {
		return result, err
	}
	bestPath := ""
	for _, p := range projects {
		if p.MachineID != machineID || !path.IsAbs(p.Path) {
			continue
		}
		root := path.Clean(p.Path)
		if sessionPath != root && !strings.HasPrefix(sessionPath, strings.TrimSuffix(root, "/")+"/") {
			continue
		}
		if !result.Matched {
			result.ProjectID, result.Matched, bestPath = p.ID, true, root
			continue
		}
		if len(root) > len(bestPath) || (len(root) == len(bestPath) && p.ID < result.ProjectID) {
			result.ProjectID, bestPath = p.ID, root
		}
	}
	return result, nil
}

// RenameSessionLink and EndSessionLink are session lifecycle hooks. They are
// called after the remote mutation succeeds and before inventory is refreshed.
func (s *Service) RenameSessionLink(ctx context.Context, machineID, oldName, newName string) error {
	err := s.repo.RenameSessionLink(ctx, machineID, oldName, newName)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	return err
}

func (s *Service) EndSessionLink(ctx context.Context, machineID, sessionName string) error {
	return s.repo.DeleteSessionLink(ctx, machineID, sessionName)
}

// Run removes links for sessions observed ended in a full sessions.changed
// snapshot. It exits when ctx is canceled.
func (s *Service) Run(ctx context.Context) {
	if s.bus == nil {
		return
	}
	ch, unsubscribe := s.bus.Subscribe(64)
	defer unsubscribe()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			if event.Type != events.SessionsChanged {
				continue
			}
			change, ok := event.Payload.(inventory.SessionsChanged)
			if !ok {
				continue
			}
			active := make([]string, 0, len(change.Sessions))
			for _, sess := range change.Sessions {
				active = append(active, sess.Name)
			}
			if err := s.repo.PruneSessionLinks(ctx, event.Machine, active); err != nil && ctx.Err() == nil {
				s.log.Warn("prune ended session links", "machine", event.Machine, "err", err)
			}
		}
	}
}
