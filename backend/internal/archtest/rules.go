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
//     with it: the protection is that nothing else is allowed to say it (ADR 0005). The test
//     evaluates whole constant string expressions ("set_" + "config(", constants built from
//     constants), undoes quotes, '||' and whitespace before looking, and searches .sql files
//     whole, comments included. It cannot see text built at run time (fmt.Sprintf).
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

// constTable holds the constants declared in a package. A name is present when it is a
// constant; val is its value when it is a string the rules can compute.
type constTable map[string]constVal

type constVal struct {
	val   string
	isStr bool
}

// touchesContext reports whether text, once the usual ways of splitting it are undone, names
// something that writes the RLS context or changes the role or the session in a way that could
// bypass it. It is the only protection of that context, so it errs on the side of reporting:
//
//   - set_config, app.team_id, app.user_id and session_replication_role are looked for with the
//     quotes, the whitespace and the characters that glue text together ('||', ',', '+', and
//     parentheses) removed, so "set_config" (quoted), 'app.' || 'team_id' and a newline before
//     the '(' do not hide them. The bare name set_config is enough: no call is needed.
//   - SET and RESET statements are looked for with the whitespace collapsed and a short gap
//     allowed between the words, so a comment between SET and LOCAL does not hide them.
//
// Reading the context through app.current_team_id() and app.current_user_id() is not matched.
// What it cannot see is text built at run time (fmt.Sprintf, strings.Join): the review of any
// new use of those with SQL is the second line.
func touchesContext(text string) bool {
	lower := strings.ToLower(text)
	compact := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '\r', '\'', '"', '`', '|', ',', '+', '(', ')':
			return -1
		}
		return r
	}, lower)
	for _, token := range []string{"set_config", "app.team_id", "app.user_id", "session_replication_role"} {
		if strings.Contains(compact, token) {
			return true
		}
	}
	spaced := whitespace.ReplaceAllString(strings.NewReplacer("'", " ", `"`, " ", "||", " ").Replace(lower), " ")
	return setStatements.MatchString(spaced)
}

var (
	whitespace    = regexp.MustCompile(`\s+`)
	setStatements = regexp.MustCompile(`\bset\b.{0,40}?\b(local|session|role)\b|\breset\b.{0,40}?\b(role|all|session)\b`)
)

// layerDir matches the directories of a module that must not import the database driver.
var layerDir = regexp.MustCompile(`^internal/[^/]+/(domain|app|transport)/`)

// poolDirs may import pgxpool.
var poolDirs = []string{dbDir, "internal/bootstrap/"}

// checkSource applies the rules to one source file. path is relative to the backend
// module (slash separated); consts are the names of the constants declared in the
// file's package.
func checkSource(path, src string, consts constTable) ([]string, error) {
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
		// A whole constant string expression is evaluated and checked as one: "set_" + "config("
		// is the same text as "set_config(". The check does not descend into it afterwards.
		if e, ok := n.(ast.Expr); ok && !inDB {
			switch e.(type) {
			case *ast.BasicLit, *ast.BinaryExpr:
				if text, ok := evalString(e, consts); ok {
					if touchesContext(text) {
						report(e.Pos(), "SQL that writes the RLS context or the role belongs in %s only (ADR 0005)", dbDir)
					}
					return false
				}
			}
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
func isFixedMessage(e ast.Expr, consts constTable) bool {
	switch v := e.(type) {
	case *ast.BasicLit:
		return v.Kind == token.STRING
	case *ast.Ident:
		_, ok := consts[v.Name]
		return ok
	default:
		return false
	}
}

// evalString computes a constant string expression: literals, constants of the package and
// their concatenation, in parentheses or not. It reports false for anything else.
func evalString(e ast.Expr, consts constTable) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		return s, err == nil
	case *ast.ParenExpr:
		return evalString(v.X, consts)
	case *ast.Ident:
		c, ok := consts[v.Name]
		return c.val, ok && c.isStr
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		l, lok := evalString(v.X, consts)
		r, rok := evalString(v.Y, consts)
		return l + r, lok && rok
	}
	return "", false
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

// packageConsts adds the constants declared in a file to the table, with the value of the string
// ones, computed from those already known (so a + b of two constants is known too). The caller
// repeats it until the table stops growing, which settles the order of declarations across files.
func packageConsts(f *ast.File, into constTable) {
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, sp := range gd.Specs {
			vs := sp.(*ast.ValueSpec)
			for i, n := range vs.Names {
				cv := constVal{}
				if i < len(vs.Values) {
					cv.val, cv.isStr = evalString(vs.Values[i], into)
				}
				if old, seen := into[n.Name]; !seen || (!old.isStr && cv.isStr) {
					into[n.Name] = cv
				}
			}
		}
	}
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
// go/ast rules cannot see them. The whole file is searched, comments included: a "--" inside a
// string literal is not a comment, and a rule that strips comments would be fooled by it. The
// price is that a comment which names the forbidden words is reported too; word it differently.
func checkSQLFile(path, text string) []string {
	if !touchesContext(text) {
		return nil
	}
	return []string{fmt.Sprintf("%s: SQL that writes the RLS context or the role belongs in %s only (ADR 0005); the whole file is searched, comments included", path, dbDir)}
}
