// Package web embeds the built Vue app (web/dist).
//
// dist always exists in the repo (a tracked .gitkeep), so the Go build works
// without building the frontend first; the server then serves a placeholder.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist returns the built SPA rooted at dist/.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // unreachable: "dist" is a valid, embedded path
	}
	return sub
}
