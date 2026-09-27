package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type finding struct {
	rule string
	line int
}

type backgroundAllowance struct {
	count  int
	reason string
}

// These roots are intentional: fsbrowse derives per-operation deadlines from
// its service lifetime; testenv owns short-lived integration fixture processes.
// Counts make new Background roots in either file fail review until documented.
var backgroundAllowlist = map[string]backgroundAllowance{
	"internal/fsbrowse/fsbrowse.go": {count: 1, reason: "service root; every SFTP operation derives its own deadline"},
	"internal/testenv/testenv.go":   {count: 5, reason: "integration fixture process roots; each external call has a bounded lifetime"},
}

func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func productionGoFiles(t *testing.T) []string {
	t.Helper()
	root := filepath.Join(repoRoot(), "internal")
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && (entry.Name() == "testdata" || strings.HasPrefix(entry.Name(), ".")) {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func inspect(path string) ([]finding, int, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, 0, err
	}
	var findings []finding
	backgrounds := 0
	add := func(rule string, node ast.Node) {
		findings = append(findings, finding{rule: rule, line: fset.Position(node.Pos()).Line})
	}
	ast.Inspect(file, func(node ast.Node) bool {
		if selector, ok := node.(*ast.SelectorExpr); ok {
			pkg, isIdent := selector.X.(*ast.Ident)
			if isIdent && pkg.Name == "http" && selector.Sel.Name == "DefaultClient" {
				add("http-timeout", selector)
			}
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, isIdent := selector.X.(*ast.Ident)
		if !isIdent {
			return true
		}
		switch {
		case pkg.Name == "exec" && selector.Sel.Name == "Command":
			add("exec-context", call)
		case pkg.Name == "http" && (selector.Sel.Name == "Get" || selector.Sel.Name == "Post" || selector.Sel.Name == "DefaultClient"):
			add("http-timeout", call)
		case pkg.Name == "context" && selector.Sel.Name == "Background":
			backgrounds++
		case pkg.Name == "websocket" && selector.Sel.Name == "Accept":
			if !functionHasReadBound(file, call.Pos()) {
				add("websocket-read-bound", call)
			}
		}
		return true
	})
	// Every explicit http.Client must set a deadline.
	ast.Inspect(file, func(node ast.Node) bool {
		lit, ok := node.(*ast.CompositeLit)
		if !ok || !isHTTPClient(lit.Type) {
			return true
		}
		for _, elt := range lit.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Timeout" {
					return true
				}
			}
		}
		add("http-timeout", lit)
		return true
	})
	return findings, backgrounds, nil
}

func isHTTPClient(expr ast.Expr) bool {
	if ptr, ok := expr.(*ast.StarExpr); ok {
		expr = ptr.X
	}
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "http" && sel.Sel.Name == "Client"
}

func functionHasReadBound(file *ast.File, pos token.Pos) bool {
	var owner *ast.FuncDecl
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Pos() <= pos && pos <= fn.End() {
			owner = fn
			break
		}
	}
	if owner == nil {
		return false
	}
	bounded := false
	ast.Inspect(owner.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if ok && (selector.Sel.Name == "SetReadLimit" || selector.Sel.Name == "CloseRead") {
			bounded = true
		}
		return true
	})
	return bounded
}

func TestProductionCallsAreBounded(t *testing.T) {
	seenBackground := map[string]int{}
	for _, path := range productionGoFiles(t) {
		rel, err := filepath.Rel(repoRoot(), path)
		if err != nil {
			t.Fatal(err)
		}
		findings, backgrounds, err := inspect(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range findings {
			t.Errorf("%s:%d violates %s", rel, f.line, f.rule)
		}
		if backgrounds == 0 {
			continue
		}
		allow, ok := backgroundAllowlist[filepath.ToSlash(rel)]
		if !ok {
			t.Errorf("%s contains %d context.Background calls without an allowlist reason", rel, backgrounds)
			continue
		}
		if strings.TrimSpace(allow.reason) == "" {
			t.Errorf("%s has no allowlist reason", rel)
		}
		if backgrounds != allow.count {
			t.Errorf("%s has %d context.Background calls, allowlisted %d (%s)", rel, backgrounds, allow.count, allow.reason)
		}
		seenBackground[filepath.ToSlash(rel)] = backgrounds
	}
	for path, allow := range backgroundAllowlist {
		if seenBackground[path] != allow.count {
			t.Errorf("allowlisted context roots changed in %s: got %d, want %d", path, seenBackground[path], allow.count)
		}
	}
}

func TestRulesRejectViolatingFixtures(t *testing.T) {
	fixtures := map[string]string{
		"unboundedexec/bad.go": "exec-context",
		"httphelper/bad.go":    "http-timeout",
		"httpdefault/bad.go":   "http-timeout",
		"httpclient/bad.go":    "http-timeout",
		"unboundedws/bad.go":   "websocket-read-bound",
	}
	for name, rule := range fixtures {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(repoRoot(), "internal", "archtest", "testdata", name)
			findings, _, err := inspect(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, got := range findings {
				if got.rule == rule {
					return
				}
			}
			t.Fatalf("fixture did not violate %s: %+v", rule, findings)
		})
	}
	bg := filepath.Join(repoRoot(), "internal", "archtest", "testdata", "background", "bad.go")
	_, count, err := inspect(bg)
	if err != nil || count != 1 {
		t.Fatalf("context.Background fixture count = %d, err = %v", count, err)
	}
}
