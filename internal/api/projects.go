package api

import (
	"context"
	"errors"
	"net/http"
	"path"
	"strings"

	"hostbud/internal/projects"
	"hostbud/internal/store"
)

// ProjectService is the authenticated API's project service surface.
type ProjectService interface {
	List(context.Context, string) ([]store.Project, error)
	Get(context.Context, string) (store.Project, error)
	Create(context.Context, string, string, string) (store.Project, error)
	Rename(context.Context, string, string) (store.Project, error)
}

type createProjectRequest struct {
	MachineID string `json:"machineId"`
	Path      string `json:"path"`
	Name      string `json:"name"`
}

type renameProjectRequest struct {
	Name string `json:"name"`
}

func (s *server) projectMachine(w http.ResponseWriter, id string) bool {
	if _, ok := s.machines[id]; !ok {
		writeError(w, http.StatusNotFound, "unknown machine", "")
		return false
	}
	return true
}

func (s *server) listProjects(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	for key, values := range q {
		if key != "machine" || len(values) != 1 || values[0] == "" {
			writeError(w, http.StatusBadRequest, "invalid query", "Use only one machine parameter.")
			return
		}
	}
	machineID := q.Get("machine")
	if machineID == "" {
		if _, ok := s.machines[store.HostMachineID]; ok {
			machineID = store.HostMachineID
		} else if len(s.order) > 0 {
			machineID = s.order[0]
		} else {
			writeError(w, http.StatusNotFound, "unknown machine", "")
			return
		}
	}
	if !s.projectMachine(w, machineID) {
		return
	}
	items, err := s.cfg.Projects.List(r.Context(), machineID)
	if err != nil {
		s.projectError(w, err)
		return
	}
	if items == nil {
		items = []store.Project{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": items})
}

func (s *server) createProject(w http.ResponseWriter, r *http.Request) {
	var req createProjectRequest
	if !decode(w, r, &req) {
		return
	}
	if req.MachineID == "" {
		req.MachineID = store.HostMachineID
	}
	if req.Path == "" || len(req.Path) > 4096 || strings.IndexByte(req.Path, 0) >= 0 || !path.IsAbs(req.Path) ||
		len(strings.TrimSpace(req.Name)) > 255 || strings.IndexByte(req.Name, 0) >= 0 {
		writeError(w, http.StatusBadRequest, "invalid project", "Use an absolute target path and a project name up to 255 bytes.")
		return
	}
	if !s.projectMachine(w, req.MachineID) {
		return
	}
	p, err := s.cfg.Projects.Create(r.Context(), req.MachineID, req.Path, req.Name)
	if err != nil {
		s.projectError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *server) getProject(w http.ResponseWriter, r *http.Request) {
	p, err := s.cfg.Projects.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.projectError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *server) renameProject(w http.ResponseWriter, r *http.Request) {
	var req renameProjectRequest
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Name) == "" || len(strings.TrimSpace(req.Name)) > 255 || strings.IndexByte(req.Name, 0) >= 0 {
		writeError(w, http.StatusBadRequest, "invalid project", "Use a non-empty project name up to 255 bytes.")
		return
	}
	p, err := s.cfg.Projects.Rename(r.Context(), r.PathValue("id"), req.Name)
	if err != nil {
		s.projectError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *server) projectError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "project not found", "Refresh the project list and try again.")
	case errors.Is(err, projects.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, "invalid project", "Use an absolute target path and a non-empty project name up to 255 bytes.")
	case strings.Contains(err.Error(), "project path must"), strings.Contains(err.Error(), "project name must"), strings.Contains(err.Error(), "session name must"):
		writeError(w, http.StatusBadRequest, "invalid project", "Use an absolute target path and valid project metadata.")
	default:
		s.cfg.Log.Error("project request failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error", "")
	}
}
