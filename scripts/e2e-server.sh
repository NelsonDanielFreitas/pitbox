#!/usr/bin/env bash
# Starts the server the browser tests (F1-14) talk to: the real pitbox-api over throwaway
# containers, with synthetic data (backend/internal/bootstrap/e2e_server_test.go, build tag
# e2e, so it is compiled into no binary). The test binary is run directly, not through
# `go test`, so that SIGTERM reaches it and its containers are removed.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out="${PITBOX_E2E_DIR:-$root/app/.e2e}"
mkdir -p "$out"
chmod 700 "$out"

(cd "$root/backend" && go test -c -tags e2e -o "$out/e2e-server.test" ./internal/bootstrap)

export PITBOX_E2E_SERVE=1
export PITBOX_E2E_STATE="${PITBOX_E2E_STATE:-$out/state.json}"
export PITBOX_REQUIRE_DOCKER=1
rm -f "$PITBOX_E2E_STATE"
cd "$root/backend/internal/bootstrap"
exec "$out/e2e-server.test" -test.run '^TestE2EServer$' -test.v -test.timeout 0
