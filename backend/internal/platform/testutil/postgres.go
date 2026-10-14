package testutil

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Postgres describes a throwaway PostgreSQL started by StartPostgres.
type Postgres struct {
	// URL connects as pitbox_api, the application role (no BYPASSRLS, member of pitbox_app).
	URL string
	// MigratorURL connects as pitbox_migrator (a member of pitbox_owner, whose sessions run as
	// the owner) and SuperURL as the postgres superuser: roles the application must never use,
	// for the tests that prove the pool refuses them.
	MigratorURL, SuperURL string
	// Exec runs SQL as the superuser, for tests that need to look behind the RLS.
	container     string
	superPassword string
}

// StartPostgres runs the image of the compose file (TimescaleDB on PostgreSQL 16, same tag
// and digest), bootstrapped by the same init script (roles, extensions, databases), then
// applies backend/db/schema/schema.sql as pitbox_migrator, exactly like scripts/db-test.sh.
// The application connects as pitbox_api, so row level security and the grants are the real ones.
//
// Skipping and failing without Docker follow StartValkey (PITBOX_REQUIRE_DOCKER=1).
func StartPostgres(t testing.TB) Postgres {
	t.Helper()
	requireDocker(t)

	root := repoRoot(t)
	image := composeImage(t, "timescale/timescaledb")
	super, migrator, api, zitadel, openfga := randomHex(t), randomHex(t), randomHex(t), randomHex(t), randomHex(t)
	env := []string{
		"POSTGRES_USER=postgres", "POSTGRES_PASSWORD=" + super, "PITBOX_MIGRATOR_PASSWORD=" + migrator,
		"PITBOX_API_PASSWORD=" + api, "ZITADEL_DB_PASSWORD=" + zitadel, "OPENFGA_DB_PASSWORD=" + openfga,
	}

	startSlots <- struct{}{}
	defer func() { <-startSlots }()
	var name, addr string
	for attempt := 1; ; attempt++ {
		name, addr = startPostgresContainer(t, root, image, env)
		if waitReachable(addr, 10*time.Second) {
			break
		}
		_ = exec.Command("docker", "rm", "-f", name).Run() //nolint:noctx,gosec // cleanup of the container this test started
		if attempt == 3 {
			t.Fatalf("postgres is not reachable from the host after %d attempts", attempt)
		}
	}

	schema, err := os.ReadFile(filepath.Join(root, "backend", "db", "schema", "schema.sql")) //nolint:gosec // fixed path inside the repository
	if err != nil {
		t.Fatalf("read schema.sql: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "exec", "-i", name, "psql", "-X", "-q", "-v", "ON_ERROR_STOP=1", //nolint:gosec // fixed arguments
		"--single-transaction", "-U", "pitbox_migrator", "-d", "pitbox")
	cmd.Stdin = bytes.NewReader(schema)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("apply schema.sql: %v: %s", err, redact(out, super, migrator, api))
	}
	return Postgres{
		URL:         fmt.Sprintf("postgres://pitbox_api:%s@%s/pitbox?sslmode=disable", api, addr),
		MigratorURL: fmt.Sprintf("postgres://pitbox_migrator:%s@%s/pitbox?sslmode=disable", migrator, addr),
		SuperURL:    fmt.Sprintf("postgres://postgres:%s@%s/pitbox?sslmode=disable", super, addr),
		container:   name, superPassword: super,
	}
}

// Exec runs one SQL statement as the superuser (bypassing RLS) and returns its output.
func (p Postgres) Exec(t testing.TB, sql string) string {
	t.Helper()
	out, err := exec.Command("docker", "exec", "-e", "PGPASSWORD="+p.superPassword, p.container, //nolint:noctx,gosec // test helper with a container this test started
		"psql", "-X", "-At", "-v", "ON_ERROR_STOP=1", "-h", "127.0.0.1", "-U", "postgres", "-d", "pitbox", "-c", sql).CombinedOutput()
	if err != nil {
		t.Fatalf("psql: %v: %s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func startPostgresContainer(t testing.TB, root, image string, env []string) (name, addr string) {
	t.Helper()
	name = "pitbox-test-pg-" + randomHex(t)[:12]
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	run := []string{"run", "-d", "--rm", "--name", name, "--security-opt", "no-new-privileges", "--tmpfs", "/var/lib/postgresql/data",
		"-v", filepath.Join(root, "deploy", "compose", "postgres", "init") + ":/docker-entrypoint-initdb.d:ro", "-p", "127.0.0.1::5432"}
	for _, e := range env {
		run = append(run, "-e", e)
	}
	run = append(run, image)
	if out, err := exec.CommandContext(ctx, "docker", run...).CombinedOutput(); err != nil { //nolint:gosec // arguments come from the repository's compose file
		t.Fatalf("docker run: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() }) //nolint:noctx,gosec // cleanup of the container this test started

	out, err := exec.CommandContext(ctx, "docker", "port", name, "5432/tcp").Output() //nolint:gosec // see above
	if err != nil {
		t.Fatalf("docker port: %v", err)
	}
	addr = strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])

	// -h 127.0.0.1: the temporary server of the init only listens on the Unix socket, so only
	// the final server (with the roles already created) answers on TCP.
	for deadline := time.Now().Add(90 * time.Second); ; time.Sleep(500 * time.Millisecond) {
		if exec.CommandContext(ctx, "docker", "exec", name, "pg_isready", "-h", "127.0.0.1", "-U", "postgres", "-d", "postgres", "-q").Run() == nil { //nolint:gosec // see above
			return name, addr
		}
		if time.Now().After(deadline) {
			logs, _ := exec.Command("docker", "logs", "--tail", "30", name).CombinedOutput() //nolint:noctx,gosec // diagnostics
			t.Fatalf("postgres did not become ready:\n%s", logs)
		}
	}
}

// composeImage returns the first image in the compose file whose name starts with prefix.
func composeImage(t testing.TB, prefix string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "deploy", "compose", "docker-compose.yml")) //nolint:gosec // fixed path inside the repository
	if err != nil {
		t.Fatalf("read compose file: %v", err)
	}
	m := regexp.MustCompile(`image:\s*(` + regexp.QuoteMeta(prefix) + `\S+@sha256:[0-9a-f]{64})`).FindSubmatch(raw)
	if m == nil {
		t.Fatalf("no image %q with a digest in the compose file", prefix)
	}
	return string(m[1])
}

func repoRoot(t testing.TB) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the repository")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..")
}

// requireDocker skips (or, with PITBOX_REQUIRE_DOCKER=1, fails) when Docker is unavailable.
func requireDocker(t testing.TB) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil || exec.Command("docker", "info").Run() != nil { //nolint:noctx,gosec // probe with fixed arguments
		if os.Getenv("PITBOX_REQUIRE_DOCKER") == "1" {
			t.Fatal("docker is required (PITBOX_REQUIRE_DOCKER=1) but not available")
		}
		t.Skip("docker not available: skipping the test that needs a real service")
	}
}
