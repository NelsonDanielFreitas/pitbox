package testutil

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	openfga "github.com/openfga/go-sdk"
	"github.com/openfga/go-sdk/client"
	"github.com/openfga/go-sdk/credentials"
)

// OpenFGA describes a throwaway OpenFGA server started by StartOpenFGA. It is
// configured with an in-memory datastore and a preshared key, exactly like a
// single-node production server, and it already has the PitBox authorization
// model loaded.
type OpenFGA struct {
	HTTPAddr     string // host:port on 127.0.0.1 for HTTP
	GRPCAddr     string // host:port on 127.0.0.1 for gRPC
	PresharedKey string
	StoreID      string
	ModelID      string
	container    string
}

// StartOpenFGA runs the same pinned OpenFGA image used by the compose file,
// creates a store and writes the PitBox authorization model (backend/authz/model.json).
//
// Without Docker the test is skipped, unless PITBOX_REQUIRE_DOCKER=1 (CI), where a
// missing Docker is a failure: a skipped security test is not a passed one.
func StartOpenFGA(t testing.TB) OpenFGA {
	t.Helper()
	requireDocker(t)

	image := composeImage(t, "openfga/openfga")
	psKey := randomHex(t)

	startSlots <- struct{}{}
	defer func() { <-startSlots }()
	var name, httpAddr, grpcAddr string
	for attempt := 1; ; attempt++ {
		name, httpAddr, grpcAddr = startOpenFGAContainer(t, image, psKey)
		if waitReachable(httpAddr, 10*time.Second) {
			break
		}
		_ = exec.Command("docker", "rm", "-f", name).Run() //nolint:noctx,gosec // cleanup of the container this test started
		if attempt == 3 {
			t.Fatalf("openfga is not reachable from the host after %d attempts", attempt)
		}
	}

	waitHealthy(t, name)

	cfg := &client.ClientConfiguration{
		ApiUrl:               "http://" + httpAddr,
		AuthorizationModelId: "", // we are about to create one
		Credentials: &credentials.Credentials{
			Method: credentials.CredentialsMethodApiToken,
			Config: &credentials.Config{ApiToken: psKey},
		},
	}
	api, err := client.NewSdkClient(cfg)
	if err != nil {
		t.Fatalf("openfga sdk client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store, err := api.CreateStore(ctx).Body(client.ClientCreateStoreRequest{Name: "pitbox-test-" + randomHex(t)[:8]}).Execute()
	if err != nil {
		t.Fatalf("create openfga store: %v", err)
	}
	storeID := store.GetId()
	if err := api.SetStoreId(storeID); err != nil {
		t.Fatalf("set store id: %v", err)
	}

	modelPath := filepath.Join(repoRoot(t), "backend", "authz", "model.json")
	modelJSON, err := os.ReadFile(modelPath) //nolint:gosec // fixed path inside the repository
	if err != nil {
		t.Fatalf("read authorization model json: %v", err)
	}
	var req openfga.WriteAuthorizationModelRequest
	if err := json.Unmarshal(modelJSON, &req); err != nil {
		t.Fatalf("parse authorization model json: %v", err)
	}
	written, err := api.WriteAuthorizationModel(ctx).Body(req).Execute()
	if err != nil {
		t.Fatalf("write authorization model: %v", err)
	}

	return OpenFGA{
		HTTPAddr:     httpAddr,
		GRPCAddr:     grpcAddr,
		PresharedKey: psKey,
		StoreID:      storeID,
		ModelID:      written.GetAuthorizationModelId(),
		container:    name,
	}
}

func startOpenFGAContainer(t testing.TB, image, psKey string) (name, httpAddr, grpcAddr string) {
	t.Helper()
	name = "pitbox-test-openfga-" + randomHex(t)[:12]
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run := []string{
		"run", "-d", "--rm", "--name", name, "--security-opt", "no-new-privileges",
		"-e", "OPENFGA_DATASTORE_ENGINE=memory",
		"-e", "OPENFGA_PLAYGROUND_ENABLED=false",
		"-e", "OPENFGA_AUTHN_METHOD=preshared",
		"-e", "OPENFGA_AUTHN_PRESHARED_KEYS=" + psKey,
		"-p", "127.0.0.1::8080",
		"-p", "127.0.0.1::8081",
		image, "run",
	}
	if out, err := exec.CommandContext(ctx, "docker", run...).CombinedOutput(); err != nil { //nolint:gosec // arguments come from the repository's compose file
		t.Fatalf("docker run: %v: %s", err, redact(out, psKey))
	}
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() }) //nolint:noctx,gosec // cleanup of the container this test started

	httpAddr = containerAddr(ctx, t, name, "8080/tcp")
	grpcAddr = containerAddr(ctx, t, name, "8081/tcp")
	return name, httpAddr, grpcAddr
}

func containerAddr(ctx context.Context, t testing.TB, name, port string) string {
	out, err := exec.CommandContext(ctx, "docker", "port", name, port).Output() //nolint:gosec // fixed arguments
	if err != nil {
		t.Fatalf("docker port %s: %v", port, err)
	}
	return strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
}

func waitHealthy(t testing.TB, name string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for {
		out, err := exec.CommandContext(ctx, "docker", "exec", name, "/usr/local/bin/grpc_health_probe", "-addr=localhost:8081").CombinedOutput() //nolint:gosec // fixed arguments
		if err == nil {
			return
		}
		if ctx.Err() != nil {
			t.Fatalf("openfga did not become healthy: %v: %s", err, out)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
