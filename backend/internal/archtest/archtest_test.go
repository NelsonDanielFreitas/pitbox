package archtest

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const header = `package p

import (
	"fmt"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
)

const fixed = "something went wrong"

var _ = fmt.Sprint
`

func TestRules(t *testing.T) {
	t.Parallel()

	consts := map[string]bool{"fixed": true}
	tests := []struct {
		name    string
		path    string
		body    string
		wantHit string // substring of the expected finding; "" = clean
	}{
		// Allowed.
		{name: "literal message", body: `func f(err error) error { return connect.NewError(connect.CodeInternal, "internal error").WithCause(err) }`},
		{name: "package constant", body: `func f(err error) error { return connect.NewError(connect.CodeInternal, fixed).WithCause(err) }`},
		{name: "positive limit in the limits file", path: "internal/platform/httpserver/rpc.go", body: `func f() { _ = connecthttp.WithReadMaxBytes(4 << 20) }`},

		// The three ways round a "no err.Error()" pattern, plus the direct one.
		{name: "err.Error() directly", body: `func f(err error) error { return connect.NewError(connect.CodeInternal, err.Error()) }`, wantHit: "string literal or a constant"},
		{name: "err.Error() through a variable", body: `func f(err error) error { msg := err.Error(); return connect.NewError(connect.CodeInternal, msg) }`, wantHit: "string literal or a constant"},
		{name: "Sprintf", body: `func f(err error) error { return connect.NewError(connect.CodeInternal, fmt.Sprintf("x: %v", err)) }`, wantHit: "string literal or a constant"},
		{name: "fmt.Sprint(err)", body: `func f(err error) error { return connect.NewError(connect.CodeInternal, fmt.Sprint(err)) }`, wantHit: "string literal or a constant"},
		{name: "string concatenation", body: `func f(s string) error { return connect.NewError(connect.CodeInternal, "x: " + s) }`, wantHit: "string literal or a constant"},
		{name: "connect.Errorf", body: `func f(err error) error { return connect.Errorf(connect.CodeInternal, "x: %v", err) }`, wantHit: "connect.Errorf"},

		// Size limits.
		{name: "a limit in another file of the same package", path: "internal/platform/httpserver/other.go", body: `func f() { _ = connecthttp.WithReadMaxBytes(1 << 20) }`, wantHit: "only in"},
		{name: "unlimited read anywhere", body: `func f() { _ = connecthttp.WithReadMaxBytes(0) }`, wantHit: "only in"},
		{name: "send limit anywhere", body: `func f() { _ = connecthttp.WithSendMaxBytes(0) }`, wantHit: "only in"},

		// Reviewed exceptions.
		{name: "allow comment with a reason on the previous line", body: "func f(err error) error {\n\t//archtest:allow message comes from a reviewed table\n\treturn connect.NewError(connect.CodeInternal, err.Error())\n}", wantHit: ""},
		{name: "allow comment without a reason does not count", body: "func f(err error) error {\n\t//archtest:allow\n\treturn connect.NewError(connect.CodeInternal, err.Error())\n}", wantHit: "string literal or a constant"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := tc.path
			if path == "" {
				path = "internal/x/x.go"
			}
			got, err := checkSource(path, header+tc.body, consts)
			if err != nil {
				t.Fatalf("checkSource: %v", err)
			}
			switch {
			case tc.wantHit == "" && len(got) != 0:
				t.Errorf("unexpected findings: %v", got)
			case tc.wantHit != "" && (len(got) == 0 || !strings.Contains(strings.Join(got, "\n"), tc.wantHit)):
				t.Errorf("findings = %v, want one containing %q", got, tc.wantHit)
			}
		})
	}
}

// An import alias must not hide a call from the rules.
func TestAliasesAreFollowed(t *testing.T) {
	t.Parallel()

	src := `package p

import (
	c "connectrpc.com/connect/v2"
	ch "connectrpc.com/connect/v2/connecthttp"
)

func f(err error) {
	_ = c.NewError(c.CodeInternal, err.Error())
	_ = ch.WithReadMaxBytes(0)
}
`
	got, err := checkSource("internal/x/x.go", src, nil)
	if err != nil {
		t.Fatalf("checkSource: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("findings = %v, want 2 (NewError and WithReadMaxBytes)", got)
	}
}

func TestDotImportsAreRefused(t *testing.T) {
	t.Parallel()

	src := "package p\n\nimport . \"connectrpc.com/connect/v2\"\n"
	got, err := checkSource("internal/x/x.go", src, nil)
	if err != nil {
		t.Fatalf("checkSource: %v", err)
	}
	if len(got) != 1 || !strings.Contains(got[0], "dot imports") {
		t.Errorf("findings = %v", got)
	}
}

// TestBackendFollowsTheRules scans the whole backend module: production code only, no
// tests and no generated code.
func TestBackendFollowsTheRules(t *testing.T) {
	root := filepath.Join("..", "..") // backend/
	constsByDir := make(map[string]map[string]bool)
	var files []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == "gen" || rel == "internal/archtest" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		files = append(files, rel)
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		dir := filepath.Dir(rel)
		if constsByDir[dir] == nil {
			constsByDir[dir] = make(map[string]bool)
		}
		for _, n := range packageConsts(f) {
			constsByDir[dir][n] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(files) < 10 {
		t.Fatalf("only %d production files found: the scan is not looking at the backend", len(files))
	}

	for _, rel := range files {
		src, err := os.ReadFile(filepath.Join(root, rel)) //nolint:gosec // paths come from the WalkDir above, inside the module
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		problems, err := checkSource(rel, string(src), constsByDir[filepath.Dir(rel)])
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range problems {
			t.Error(p)
		}
	}
}
