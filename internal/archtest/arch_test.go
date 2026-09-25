// Package archtest enforces package boundaries from docs/ARCHITECTURE.md.
package archtest

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// allowed maps a restricted import to the only packages (dirs relative to the
// repo root) that may use it in non-test code.
var allowed = map[string][]string{
	// store is the only place with SQL.
	"database/sql":                   {"internal/store"},
	"github.com/jackc/pgx/v5/stdlib": {"internal/store"},
	"github.com/pressly/goose/v3":    {"internal/store"},
	// Remote commands only through sshx; term runs the interactive attach.
	// testenv is integration-test infrastructure (ssh-agent, ssh-keygen).
	"os/exec": {"internal/sshx", "internal/term", "internal/store", "internal/testenv"},
}

func TestRestrictedImports(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", ".cache", ".git", "web", "test":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, filepath.Dir(path))
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			pkgs, restricted := allowed[p]
			if restricted && !contains(pkgs, filepath.ToSlash(rel)) {
				t.Errorf("%s imports %q; only %v may", filepath.ToSlash(rel), p, pkgs)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
