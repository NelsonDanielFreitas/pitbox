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

func TestRLSContextAndTransactionRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, path, src, wantHit string
	}{
		// Rule 3: the context has one writer.
		{name: "set_config of the team outside db", path: "internal/garage/store/cars.go", wantHit: "RLS context",
			src: "package p\nconst q = `SELECT set_config('app.team_id', $1, true)`\n"},
		{name: "SET LOCAL in an ordinary string", path: "internal/garage/store/cars.go", wantHit: "RLS context",
			src: "package p\nvar q = \"SET LOCAL app.team_id = 'x'\"\n"},
		{name: "SET ROLE", path: "internal/garage/store/cars.go", wantHit: "RLS context", src: "package p\nvar q = \"set role pitbox_owner\"\n"},
		{name: "RESET ALL", path: "internal/garage/store/cars.go", wantHit: "RLS context", src: "package p\nvar q = \"RESET ALL\"\n"},
		{name: "app.user_id alone", path: "internal/x/x.go", wantHit: "RLS context", src: "package p\nvar q = \"SELECT current_setting('app.user_id')\"\n"},
		{name: "session_replication_role", path: "internal/x/x.go", wantHit: "RLS context", src: "package p\nvar q = \"SET session_replication_role = replica\"\n"},
		{name: "split over a concatenation", path: "internal/x/x.go", wantHit: "RLS context", src: "package p\nvar q = \"SELECT set_config(\" + \"'app.team_id', $1, true)\"\n"},
		{name: "the same strings in db are fine", path: "internal/platform/db/tx.go", src: "package p\nconst q = `SELECT set_config('app.team_id', $1, true)`\n"},
		{name: "reading the context is fine", path: "internal/platform/audit/team.go", src: "package p\nconst q = `SELECT app.current_team_id(), app.current_user_id()::text`\n"},
		{name: "ordinary SQL is fine", path: "internal/garage/store/cars.go", src: "package p\nconst q = `SELECT id FROM garage.car WHERE team_id = $1`\n"},
		{name: "a reviewed exception", path: "internal/x/x.go", src: "package p\n//archtest:allow one-off migration helper reviewed in PR 123\nvar q = \"RESET ALL\"\n"},

		// Rule 4: transactions and pools in one place.
		{name: "Begin outside db", path: "internal/garage/store/cars.go", wantHit: "Begin only in", src: "package p\nfunc f(p interface{ Begin(int) }) { p.Begin(1) }\n"},
		{name: "BeginTx on a field", path: "internal/garage/app/x.go", wantHit: "BeginTx only in", src: "package p\ntype s struct{ pool interface{ BeginTx(int) } }\nfunc (x s) f() { x.pool.BeginTx(1) }\n"},
		{name: "Begin inside db", path: "internal/platform/db/tx.go", src: "package p\nfunc f(p interface{ Begin(int) }) { p.Begin(1) }\n"},
		{name: "pgxpool in a module", path: "internal/garage/store/cars.go", wantHit: "pgxpool only in", src: "package p\nimport _ \"github.com/jackc/pgx/v5/pgxpool\"\n"},
		{name: "pgxpool in the bootstrap", path: "internal/bootstrap/auth.go", src: "package p\nimport _ \"github.com/jackc/pgx/v5/pgxpool\"\n"},
		{name: "pgxpool in db", path: "internal/platform/db/db.go", src: "package p\nimport _ \"github.com/jackc/pgx/v5/pgxpool\"\n"},

		// Rule 5: layers.
		{name: "pgx in domain", path: "internal/garage/domain/car.go", wantHit: "database driver", src: "package p\nimport _ \"github.com/jackc/pgx/v5\"\n"},
		{name: "pgconn in app", path: "internal/garage/app/svc.go", wantHit: "database driver", src: "package p\nimport _ \"github.com/jackc/pgx/v5/pgconn\"\n"},
		{name: "database/sql in transport", path: "internal/garage/transport/h.go", wantHit: "database driver", src: "package p\nimport _ \"database/sql\"\n"},
		{name: "pgx in store is fine", path: "internal/garage/store/cars.go", src: "package p\nimport _ \"github.com/jackc/pgx/v5\"\n"},
		{name: "pgx in platform is fine", path: "internal/platform/audit/audit.go", src: "package p\nimport _ \"github.com/jackc/pgx/v5/pgconn\"\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := checkSource(tc.path, tc.src, nil)
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

// The .sql files that sqlc reads are not Go: the go/ast rules do not see them.
func TestSQLFilesCannotWriteTheRLSContext(t *testing.T) {
	t.Parallel()

	bad := map[string]string{
		"set_config":      "-- name: X :exec\nSELECT set_config('app.team_id', $1, true);",
		"SET LOCAL":       "SET LOCAL app.user_id = '1';",
		"SET ROLE":        "SET ROLE pitbox_owner;",
		"multi-line":      "SELECT 1;\nSELECT\n  set_config(\n 'x', 'y', false);",
		"after a comment": "SELECT 1; -- harmless\nRESET ALL;",
	}
	for name, text := range bad {
		if got := checkSQLFile("internal/x/store/queries/q.sql", text); len(got) == 0 {
			t.Errorf("%s: not detected", name)
		}
	}
	good := map[string]string{
		"plain":                  "-- name: ListCars :many\nSELECT * FROM garage.car WHERE team_id = $1;",
		"a comment that says it": "-- never SET LOCAL app.team_id here\nSELECT 1;",
		"reading the context":    "SELECT app.current_team_id();",
	}
	for name, text := range good {
		if got := checkSQLFile("internal/x/store/queries/q.sql", text); len(got) != 0 {
			t.Errorf("%s: false positive: %v", name, got)
		}
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

	// The queries sqlc reads (rule 3).
	for _, pattern := range []string{"internal/*/store/queries/*.sql", "internal/platform/*/queries/*.sql"} {
		sqlFiles, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(pattern)))
		if err != nil {
			t.Fatalf("glob %s: %v", pattern, err)
		}
		for _, f := range sqlFiles {
			text, err := os.ReadFile(f) //nolint:gosec // paths come from the Glob above, inside the module
			if err != nil {
				t.Fatalf("read %s: %v", f, err)
			}
			rel, _ := filepath.Rel(root, f)
			for _, p := range checkSQLFile(filepath.ToSlash(rel), string(text)) {
				t.Error(p)
			}
		}
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
