// Package testutil has helpers for tests that need real services.
package testutil

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Valkey describes a throwaway Valkey started by StartValkey.
type Valkey struct {
	Addr string // host:port on 127.0.0.1
	// Username and Password are the application's ACL user (the one pitbox-api uses).
	Username, Password string
	// AdminPassword is the password of the "default" user, for tests that need to look
	// at what the application wrote (the application user cannot run KEYS, for example).
	AdminPassword string
	container     string
}

// StartValkey runs a Valkey configured exactly like the compose one: the image (tag and
// digest) and the full command line, ACL rules included, are read from
// deploy/compose/docker-compose.yml, so a test that passes proves the real ACL works with
// the real client.
//
// Without Docker the test is skipped, unless PITBOX_REQUIRE_DOCKER=1 (CI), where a missing
// Docker is a failure: a skipped security test is not a passed one.
func StartValkey(t testing.TB) Valkey {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil || exec.Command("docker", "info").Run() != nil { //nolint:noctx,gosec // probe with fixed arguments
		if os.Getenv("PITBOX_REQUIRE_DOCKER") == "1" {
			t.Fatal("docker is required (PITBOX_REQUIRE_DOCKER=1) but not available")
		}
		t.Skip("docker not available: skipping the test that needs a real Valkey")
	}

	image, args := composeValkey(t)
	adminPw, appPw := randomHex(t), randomHex(t)
	subst := map[string]string{"VALKEY_PASSWORD": adminPw, "VALKEY_API_PASSWORD": appPw}
	for i, a := range args {
		args[i] = placeholder.ReplaceAllStringFunc(a, func(m string) string {
			name := placeholder.FindStringSubmatch(m)[1]
			v, ok := subst[name]
			if !ok {
				t.Fatalf("compose command uses an unknown variable %s", name)
			}
			return v
		})
	}

	// Docker Desktop (WSL) sometimes loses the host port forward when many containers start at
	// once. Starts are limited to a few at a time and one that cannot be reached is replaced.
	startSlots <- struct{}{}
	defer func() { <-startSlots }()
	var name, addr string
	for attempt := 1; ; attempt++ {
		name, addr = startContainer(t, image, args, adminPw, appPw)
		if waitReachable(addr, 10*time.Second) {
			break
		}
		_ = exec.Command("docker", "rm", "-f", name).Run() //nolint:noctx,gosec // cleanup of the container this test started
		if attempt == 3 {
			t.Fatalf("valkey is not reachable from the host after %d attempts", attempt)
		}
	}
	return Valkey{Addr: addr, Username: "pitbox_api", Password: appPw, AdminPassword: adminPw, container: name}
}

var placeholder = regexp.MustCompile(`\$\{(\w+)[^}]*\}`)

// composeValkey returns the image and the arguments of the valkey service's command.
func composeValkey(t testing.TB) (image string, args []string) {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "deploy", "compose", "docker-compose.yml")
	raw, err := os.ReadFile(path) //nolint:gosec // fixed path inside the repository
	if err != nil {
		t.Fatalf("read compose file: %v", err)
	}
	lines := strings.Split(string(raw), "\n")
	in := false
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		switch {
		case l == "  valkey:":
			in = true
		case in && len(l) > 2 && l[0] == ' ' && l[1] == ' ' && l[2] != ' ' && !strings.HasPrefix(l, "  #"):
			in = false // next service
		case in && strings.HasPrefix(l, "    image: "):
			image = strings.TrimSpace(strings.TrimPrefix(l, "    image: "))
		case in && l == "    command:":
			for i++; i < len(lines) && strings.HasPrefix(lines[i], "      - "); i++ {
				a := strings.TrimSpace(strings.TrimPrefix(lines[i], "      - "))
				if strings.HasPrefix(a, `"`) && strings.HasSuffix(a, `"`) {
					a = a[1 : len(a)-1]
				}
				args = append(args, a)
			}
			i--
		}
	}
	if image == "" || len(args) == 0 {
		t.Fatalf("could not read the valkey service from %s", path)
	}
	return image, args
}

func randomHex(t testing.TB) string {
	t.Helper()
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("random: %v", err)
	}
	return hex.EncodeToString(b)
}

func redact(out []byte, secrets ...string) string {
	s := string(out)
	for _, sec := range secrets {
		s = strings.ReplaceAll(s, sec, "<redacted>")
	}
	return s
}

// startSlots bounds how many containers are being started at the same time.
var startSlots = make(chan struct{}, 4)

// startContainer runs the container, waits until the server answers inside it and returns its
// name and host address. The container is removed when the test ends.
func startContainer(t testing.TB, image string, args []string, adminPw, appPw string) (name, addr string) {
	t.Helper()
	name = "pitbox-test-valkey-" + randomHex(t)[:12]
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run := append([]string{"run", "-d", "--rm", "--name", name, "--security-opt", "no-new-privileges",
		"-p", "127.0.0.1::6379", image}, args...)
	if out, err := exec.CommandContext(ctx, "docker", run...).CombinedOutput(); err != nil { //nolint:gosec // arguments come from the repository's compose file
		t.Fatalf("docker run: %v: %s", err, redact(out, adminPw, appPw))
	}
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() }) //nolint:noctx,gosec // cleanup of the container this test started

	out, err := exec.CommandContext(ctx, "docker", "port", name, "6379/tcp").Output() //nolint:gosec // see above
	if err != nil {
		t.Fatalf("docker port: %v", err)
	}
	addr = strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])

	for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(200 * time.Millisecond) {
		cmd := exec.CommandContext(ctx, "docker", "exec", "-e", "REDISCLI_AUTH="+adminPw, name, "valkey-cli", "ping") //nolint:gosec // see above
		if o, err := cmd.Output(); err == nil && bytes.Contains(o, []byte("PONG")) {
			return name, addr
		}
		if time.Now().After(deadline) {
			t.Fatal("valkey did not become ready")
		}
	}
}

// waitReachable reports whether a TCP connection to addr works within the timeout.
func waitReachable(addr string, timeout time.Duration) bool {
	for deadline := time.Now().Add(timeout); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(context.Background(), "tcp", addr); err == nil {
			_ = conn.Close()
			return true
		}
	}
	return false
}
