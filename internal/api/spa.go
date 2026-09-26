package api

import (
	"bytes"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

// placeholderHTML is served when the binary was built without the frontend.
const placeholderHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>HostBud 🚀</title></head>
<body style="background:#0f1115;color:#d7dae0;font-family:monospace;padding:2rem">
<h1>hostbud</h1>
<p>The web UI was not built into this binary. Run <code>make build</code> (or <code>make deploy</code>).</p>
</body></html>
`

// spaHandler serves static files from dist and falls back to index.html for
// client-side routes. /api/ and /ws/ paths never fall back. Mount it on "GET /".
func spaHandler(dist fs.FS) http.Handler {
	files := http.FileServerFS(dist)
	index, err := fs.ReadFile(dist, "index.html")
	built := err == nil

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/ws/") {
			http.NotFound(w, r)
			return
		}
		if !built {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			_, _ = w.Write([]byte(placeholderHTML))
			return
		}

		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name != "" && name != "index.html" {
			if st, err := fs.Stat(dist, name); err == nil && !st.IsDir() {
				if strings.HasPrefix(name, "assets/") {
					// Vite content-hashes everything under assets/.
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			if path.Ext(name) != "" {
				// A missing asset is a 404, not the app shell.
				http.NotFound(w, r)
				return
			}
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(index))
	})
}
