package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"syscall"

	"hostbud/internal/fsbrowse"
)

// FileBrowser is the authenticated API's read/list/create-only SFTP surface.
type FileBrowser interface {
	Home(ctx context.Context) (string, error)
	List(ctx context.Context, path string, hidden bool) (string, []fsbrowse.Entry, error)
	Stat(ctx context.Context, path string) (fsbrowse.StatResult, error)
	Mkdir(ctx context.Context, parent, name string) (string, error)
	Upload(ctx context.Context, directory, name string, source io.Reader, size int64) (string, error)
}

const maxFSPathQuery = fsbrowse.MaxPathBytes

func (s *server) fsMachine(w http.ResponseWriter, r *http.Request) bool {
	if _, ok := s.machine(w, r); !ok {
		return false
	}
	return true
}

func (s *server) fsHome(w http.ResponseWriter, r *http.Request) {
	if !s.fsMachine(w, r) {
		return
	}
	if !onlyFSQueryKeys(r, nil) {
		writeError(w, http.StatusBadRequest, "unexpected query parameter", "This endpoint does not take query parameters.")
		return
	}
	home, err := s.cfg.FileSystem.Home(r.Context())
	if err != nil {
		s.writeFilesystemError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": home})
}

func fsQuery(r *http.Request, key string) (string, bool) {
	values, ok := r.URL.Query()[key]
	if !ok {
		return "", true
	}
	if len(values) != 1 || len(values[0]) > maxFSPathQuery || strings.IndexByte(values[0], 0) >= 0 {
		return "", false
	}
	return values[0], true
}

func onlyFSQueryKeys(r *http.Request, allowed []string) bool {
	query := r.URL.Query()
	for key := range query {
		found := false
		for _, candidate := range allowed {
			if key == candidate {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (s *server) fsList(w http.ResponseWriter, r *http.Request) {
	if !s.fsMachine(w, r) {
		return
	}
	if !onlyFSQueryKeys(r, []string{"path", "hidden"}) {
		writeError(w, http.StatusBadRequest, "unexpected query parameter", "Use only path and hidden.")
		return
	}
	path, ok := fsQuery(r, "path")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid path", "Use one target path no longer than 4096 bytes.")
		return
	}
	hiddenText, ok := fsQuery(r, "hidden")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid hidden option", "Use hidden=true or hidden=false.")
		return
	}
	hidden := false
	if hiddenText != "" {
		if hiddenText != "true" && hiddenText != "false" {
			writeError(w, http.StatusBadRequest, "invalid hidden option", "Use hidden=true or hidden=false.")
			return
		}
		hidden, _ = strconv.ParseBool(hiddenText)
	}
	var full string
	var entries []fsbrowse.Entry
	var truncated bool
	var err error
	if paged, ok := s.cfg.FileSystem.(interface {
		ListPage(context.Context, string, bool) (string, []fsbrowse.Entry, bool, error)
	}); ok {
		full, entries, truncated, err = paged.ListPage(r.Context(), path, hidden)
	} else {
		full, entries, err = s.cfg.FileSystem.List(r.Context(), path, hidden)
	}
	if err != nil {
		s.writeFilesystemError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": full, "entries": entries, "truncated": truncated})
}

func (s *server) fsStat(w http.ResponseWriter, r *http.Request) {
	if !s.fsMachine(w, r) {
		return
	}
	if !onlyFSQueryKeys(r, []string{"path"}) {
		writeError(w, http.StatusBadRequest, "unexpected query parameter", "Use only path.")
		return
	}
	path, ok := fsQuery(r, "path")
	if !ok || path == "" {
		writeError(w, http.StatusBadRequest, "invalid path", "Provide one target path no longer than 4096 bytes.")
		return
	}
	result, err := s.cfg.FileSystem.Stat(r.Context(), path)
	if err != nil {
		s.writeFilesystemError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type mkdirRequest struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

func (s *server) fsMkdir(w http.ResponseWriter, r *http.Request) {
	if !s.fsMachine(w, r) {
		return
	}
	if !onlyFSQueryKeys(r, nil) {
		writeError(w, http.StatusBadRequest, "unexpected query parameter", "Send path and name in the JSON body.")
		return
	}
	var req mkdirRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Path == "" || len(req.Path) > maxFSPathQuery || len(req.Name) > fsbrowse.MaxNameBytes {
		writeError(w, http.StatusBadRequest, "invalid path or directory name", "Provide a selected directory and one child name.")
		return
	}
	full, err := s.cfg.FileSystem.Mkdir(r.Context(), req.Path, req.Name)
	if err != nil {
		s.writeFilesystemError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"path": full})
}

func (s *server) fsUpload(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.machine(w, r); !ok {
		return
	}
	if !onlyFSQueryKeys(r, []string{"directory", "name"}) {
		writeError(w, http.StatusBadRequest, "unexpected query parameter", "Use only directory and name.")
		return
	}
	directory, directoryOK := fsQuery(r, "directory")
	name, nameOK := fsQuery(r, "name")
	if !directoryOK || !nameOK || directory == "" || name == "" {
		writeError(w, http.StatusBadRequest, "invalid upload destination", "Provide one target directory and file name.")
		return
	}
	if r.ContentLength < 0 || r.ContentLength > fsbrowse.MaxUploadBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "photo exceeds the 100 MiB upload limit", "Choose a smaller original photo.")
		return
	}
	full, err := s.cfg.FileSystem.Upload(r.Context(), directory, name, r.Body, r.ContentLength)
	if err != nil {
		s.writeFilesystemError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"path": full, "size": r.ContentLength})
}

func (s *server) writeFilesystemError(w http.ResponseWriter, err error) {
	var operation *fsbrowse.Error
	if errors.As(err, &operation) && operation.Timeout > 0 {
		writeError(w, http.StatusGatewayTimeout,
			"The host's file service didn't answer within "+operation.Timeout.String(),
			"Try again; if it keeps happening, check that sftp-server works: `ssh <host> -s sftp`.")
		return
	}
	switch {
	case errors.Is(err, fsbrowse.ErrInvalidPath):
		writeError(w, http.StatusBadRequest, "invalid path", "Use an absolute or home-relative target path.")
	case errors.Is(err, fsbrowse.ErrInvalidName):
		writeError(w, http.StatusBadRequest, "invalid directory name", "Use a single child name without separators, . or .. .")
	case errors.Is(err, fsbrowse.ErrTooManyEntries):
		writeError(w, http.StatusRequestEntityTooLarge, "directory has too many entries", "Choose a smaller directory.")
	case errors.Is(err, context.Canceled):
		writeError(w, http.StatusRequestTimeout, "filesystem request canceled", "Try the request again.")
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, "filesystem request timed out", "The host may be busy or unreachable; try again.")
	case fsbrowse.IsNotExist(err):
		writeError(w, http.StatusNotFound, "path not found", "Check the path and try again.")
	case errors.Is(err, syscall.ENOTDIR):
		writeError(w, http.StatusBadRequest, "path is not a directory", "Choose an existing directory.")
	case errors.Is(err, fsbrowse.ErrNotDirectory):
		writeError(w, http.StatusBadRequest, "path is not a directory", "Choose an existing directory.")
	case errors.Is(err, fsbrowse.ErrAlreadyExists):
		if operation != nil && operation.Op == "create directory" {
			writeError(w, http.StatusConflict, "directory already exists", "Choose another name or open the existing directory.")
		} else {
			writeError(w, http.StatusConflict, "a file or directory with that name already exists", "Rename the photo before sending it.")
		}
	case errors.Is(err, fsbrowse.ErrUploadTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "photo exceeds the 100 MiB upload limit", "Choose a smaller original photo.")
	case errors.Is(err, fsbrowse.ErrFileSizeMismatch):
		writeError(w, http.StatusBadRequest, "photo upload was incomplete", "Retry sending the original photo.")
	case fsbrowse.IsPermission(err):
		writeError(w, http.StatusForbidden, "permission denied", "The target user cannot access this path.")
	default:
		writeError(w, http.StatusServiceUnavailable, "filesystem unavailable", "Check that SSH and SFTP are available on the host, then try again.")
	}
}
