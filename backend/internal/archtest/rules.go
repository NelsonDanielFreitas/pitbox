// Package archtest holds architecture rules that a linter cannot express, checked on
// the Go syntax tree by ordinary tests (`make test`), with no extra dependency.
//
// The rules protect the two decisions of ADR 0012 that are easy to break by accident:
//
//  1. Public error messages are written on purpose. connect.NewError must get a string
//     literal or a constant declared in the same package as its message, never a
//     variable, a call such as err.Error() or fmt.Sprintf, because that text goes to
//     the client. connect.Errorf is not allowed in production code. This is an
//     allow-list, not a list of forbidden patterns: `msg := err.Error()` followed by
//     connect.NewError(code, msg) is refused too.
//
//  2. Message size limits are never "unlimited" by mistake. connecthttp.WithReadMaxBytes
//     and WithSendMaxBytes may only be called in internal/platform/httpserver/rpc.go,
//     which uses a positive constant (0 means "no limit").
//
//  3. The row-level-security context has one writer. SQL that sets app.user_id or
//     app.team_id (set_config, SET LOCAL, SET ROLE, RESET ...) may only be written in
//     internal/platform/db, in Go strings or in the .sql files sqlc reads. The context is a
//     plain session variable, so any SQL that runs as the application role could switch team
//     with it: the protection is that nothing else is allowed to say it (ADR 0005).
//
//  4. Transactions are opened in one place. Begin and BeginTx only in internal/platform/db
//     (a transaction opened elsewhere has no team context), and the pool package
//     (pgxpool) only there and in the bootstrap.
//
//  5. The layers of a module do not import the database driver: domain, app and transport
//     reach the database through ports and db.DBTX (CLAUDE.md, architecture rule 1).
//
// A reviewed exception is a comment on the line before (or on the same line):
//
//	//archtest:allow <reason>
//
// The reason is mandatory.
package archtest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	connectImport     = "connectrpc.com/connect/v2"
	connecthttpImport = "connectrpc.com/connect/v2/connecthttp"

	// limitsFile is the only file allowed to set message size limits.
	limitsFile = "internal/platform/httpserver/rpc.go"

	// dbDir is the only place that writes the RLS context and opens transactions.
	dbDir = "internal/platform/db/"

	pgxPrefix     = "github.com/jackc/pgx/"
	pgxpoolImport = "github.com/jackc/pgx/v5/pgxpool"
)

// contextSQL matches SQL that writes the RLS context or changes the role or the session in a
// way that could bypass it. app.current_team_id() and app.current_user_id() (reading it) are
// not matched.
var contextSQL = regexp.MustCompile(`(?is)\bapp\.(team_id|user_id)\b|\bset_config\s*\(|\bset\s+(local|session)\b|` +
	`\bset\s+(session\s+authorization|role)\b|\breset\s+(role|all|session)\b|\bsession_replication_role\b`)

// layerDir matches the directories of a module that must not import the database driver.
var layerDir = regexp.MustCompile(`^internal/[^/]+/(domain|app|transport)/`)

// poolDirs may import pgxpool.
var poolDirs = []string{dbDir, "internal/bootstrap/"}

// checkSource applies the rules to one source file. path is relative to the backend
// module (slash separated); consts are the names of the constants declared in the
// file's package.
func checkSource(path, src string, consts map[string]bool) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	connectName, connecthttpName := importName(f, connectImport), importName(f, connecthttpImport)
	allowed := allowedLines(fset, f)

	var problems []string
	report := func(pos token.Pos, format string, args ...any) {
		line := fset.Position(pos).Line
		if allowed[line] || allowed[line-1] {
			return
		}
		problems = append(problems, fmt.Sprintf("%s:%d: %s", path, line, fmt.Sprintf(format, args...)))
	}

	slashPath := filepath.ToSlash(path)
	inDB := strings.HasPrefix(slashPath, dbDir)
	for _, imp := range f.Imports {
		ip := importPath(imp)
		if imp.Name != nil && imp.Name.Name == "." && (ip == connectImport || ip == connecthttpImport) {
			report(imp.Pos(), "dot imports of connect are not allowed: the rules cannot follow them")
		}
		if (strings.HasPrefix(ip, pgxPrefix) || ip == "database/sql") && layerDir.MatchString(slashPath) {
			report(imp.Pos(), "domain, app and transport do not import the database driver (%s): use ports and db.DBTX", ip)
		}
		if ip == pgxpoolImport && !hasAnyPrefix(slashPath, poolDirs) {
			report(imp.Pos(), "pgxpool only in %s: a pool used elsewhere skips the transaction context", strings.Join(poolDirs, " and "))
		}
	}

	ast.Inspect(f, func(n ast.Node) bool {
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING && !inDB {
			if text, err := strconv.Unquote(lit.Value); err == nil && contextSQL.MatchString(text) {
				report(lit.Pos(), "SQL that writes the RLS context or the role belongs in %s only (ADR 0005)", dbDir)
			}
			return true
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && (sel.Sel.Name == "Begin" || sel.Sel.Name == "BeginTx") && !inDB {
			report(call.Pos(), "%s only in %s: a transaction opened elsewhere has no team context", sel.Sel.Name, dbDir)
		}
		pkg, fn, ok := selector(call.Fun)
		if !ok {
			return true
		}
		switch {
		case pkg == connectName && fn == "NewError":
			if len(call.Args) != 2 || !isFixedMessage(call.Args[1], consts) {
				report(call.Pos(), "connect.NewError message must be a string literal or a constant of this package: it is sent to the client (ADR 0012)")
			}
		case pkg == connectName && fn == "Errorf":
			report(call.Pos(), "connect.Errorf formats text for the client: use connect.NewError with a fixed message and WithCause (ADR 0012)")
		case pkg == connecthttpName && (fn == "WithReadMaxBytes" || fn == "WithSendMaxBytes"):
			if filepath.ToSlash(path) != limitsFile {
				report(call.Pos(), "connecthttp.%s only in %s (zero means unlimited; ADR 0012)", fn, limitsFile)
			}
		}
		return true
	})
	return problems, nil
}

// importName returns the name a file uses for an import path ("" if not imported).
func importName(f *ast.File, path string) string {
	for _, imp := range f.Imports {
		if importPath(imp) != path {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		// The default name is the last path element, except for a version suffix
		// ("connectrpc.com/connect/v2" is package connect).
		parts := strings.Split(path, "/")
		last := parts[len(parts)-1]
		if len(last) > 1 && last[0] == 'v' && isDigits(last[1:]) && len(parts) > 1 {
			return parts[len(parts)-2]
		}
		return last
	}
	return ""
}

func importPath(imp *ast.ImportSpec) string {
	p, _ := strconv.Unquote(imp.Path.Value)
	return p
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// selector returns pkg and name for a call of the form pkg.Name(...).
func selector(fun ast.Expr) (pkg, name string, ok bool) {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return "", "", false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", "", false
	}
	return id.Name, sel.Sel.Name, true
}

// isFixedMessage reports whether e is a string literal or the name of a constant
// declared in the package.
func isFixedMessage(e ast.Expr, consts map[string]bool) bool {
	switch v := e.(type) {
	case *ast.BasicLit:
		return v.Kind == token.STRING
	case *ast.Ident:
		return consts[v.Name]
	default:
		return false
	}
}

// allowedLines returns the lines covered by a "//archtest:allow <reason>" comment.
func allowedLines(fset *token.FileSet, f *ast.File) map[int]bool {
	lines := make(map[int]bool)
	for _, cg := range f.Comments {
		for _, c := range cg.List {
			rest, ok := strings.CutPrefix(c.Text, "//archtest:allow")
			if ok && strings.TrimSpace(rest) != "" {
				lines[fset.Position(c.Pos()).Line] = true
			}
		}
	}
	return lines
}

// packageConsts returns the names of the constants declared in a file.
func packageConsts(f *ast.File) []string {
	var names []string
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, s := range gd.Specs {
			for _, n := range s.(*ast.ValueSpec).Names {
				names = append(names, n.Name)
			}
		}
	}
	return names
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// checkSQLFile applies rule 3 to a .sql file read by sqlc (queries and their schema): the
// go/ast rules cannot see them. SQL comments are ignored.
func checkSQLFile(path, text string) []string {
	var problems []string
	for i, line := range strings.Split(text, "\n") {
		if before, _, found := strings.Cut(line, "--"); found {
			line = before
		}
		if contextSQL.MatchString(line) {
			problems = append(problems, fmt.Sprintf("%s:%d: SQL that writes the RLS context or the role belongs in %s only (ADR 0005)", path, i+1, dbDir))
		}
	}
	return problems
}
