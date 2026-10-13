.DEFAULT_GOAL := help
.PHONY: help tools-check init up down reset ps logs health s3-smoke gen gen-check check-tool-versions lint lint-proto lint-go breaking test app-typecheck secrets vuln workflows ci hooks db-validate db-lint db-test db-new db-migrate run-api smoke-api zitadel-provision test-scripts

COMPOSE_DIR := deploy/compose
ENV_FILE    := $(COMPOSE_DIR)/.env
# Data de validade do PAT do utilizador de provisionamento do Zitadel (30 dias). Só conta no
# primeiro arranque do volume; o compose exige-a, por isso passa sempre pelo make.
export ZITADEL_PROVISIONER_PAT_EXPIRATION ?= $(shell date -u -d '+30 days' +%Y-%m-%dT%H:%M:%SZ)
COMPOSE     := docker compose --project-directory $(COMPOSE_DIR) --env-file $(ENV_FILE) -f $(COMPOSE_DIR)/docker-compose.yml

help: ## Mostra os alvos disponíveis
	@awk 'BEGIN {FS = ":.*## "} /^[a-z0-9-]+:.*## / {printf "  %-12s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

tools-check: ## Verifica a toolchain declarada em mise.toml
	@mise ls --current --missing | grep . && { echo "Ferramentas em falta: corre 'mise install'"; exit 1; } || true
	@go version
	@node --version
	@pnpm --version
	@rustc --version
	@buf --version
	@sqlc version
	@atlas version | head -1
	@golangci-lint --version
	@docker --version || { echo "Docker não disponível: ativa a integração WSL no Docker Desktop"; exit 1; }

init: ## Cria deploy/compose/.env com segredos aleatórios; acrescenta as chaves em falta (nunca altera as existentes)
	@umask 077; touch $(ENV_FILE); chmod 600 $(ENV_FILE); \
	add() { grep -q "^$$1=" $(ENV_FILE) || { echo "$$1=$$2" >> $(ENV_FILE); echo "  + $$1"; }; }; \
	add POSTGRES_SUPERUSER_PASSWORD "$$(openssl rand -hex 24)"; \
	add PITBOX_MIGRATOR_PASSWORD "$$(openssl rand -hex 24)"; \
	add PITBOX_API_PASSWORD "$$(openssl rand -hex 24)"; \
	add ZITADEL_DB_PASSWORD "$$(openssl rand -hex 24)"; \
	add OPENFGA_DB_PASSWORD "$$(openssl rand -hex 24)"; \
	add OPENFGA_PRESHARED_KEY "$$(openssl rand -hex 24)"; \
	add VALKEY_PASSWORD "$$(openssl rand -hex 24)"; \
	add VALKEY_API_PASSWORD "$$(openssl rand -hex 24)"; \
	add ZITADEL_MASTERKEY "$$(openssl rand -hex 16)"; \
	add ZITADEL_LOGIN_COOKIE_SECRET "$$(openssl rand -hex 32)"; \
	add ZITADEL_ADMIN_USERNAME "zitadel-admin"; \
	add ZITADEL_ADMIN_PASSWORD "$$(openssl rand -hex 12)A1!"; \
	add S3_ACCESS_KEY "$$(openssl rand -hex 10)"; \
	add S3_SECRET_KEY "$$(openssl rand -hex 24)"; \
	echo "$(ENV_FILE): permissões 600, fora do Git."

up: ## Levanta todo o ambiente local e espera pelos health checks
	@test -f $(ENV_FILE) || { echo "Falta $(ENV_FILE): corre 'make init'."; exit 1; }
	$(COMPOSE) up -d --wait

down: ## Para os contentores (mantém os volumes)
	$(COMPOSE) down

reset: ## APAGA OS VOLUMES (Postgres, S3...) e recria. Obrigatório se mudares segredos no .env
	@if [ "$(CONFIRM)" != "yes" ]; then \
	  printf "Isto apaga TODOS os volumes (Postgres, Zitadel, SeaweedFS). Escreve 'yes' para continuar: "; \
	  read ans; [ "$$ans" = "yes" ] || { echo "Cancelado."; exit 1; }; \
	fi
	$(COMPOSE) down -v
	$(MAKE) up

ps: ## Estado dos serviços
	$(COMPOSE) ps

logs: ## Logs (make logs s=<serviço>)
	$(COMPOSE) logs -f --tail=100 $(s)

health: ## Estado dos serviços e sonda ao Jaeger (sem health check no contentor)
	@$(COMPOSE) ps --format 'table {{.Service}}\t{{.Status}}'
	@set -a; . ./$(ENV_FILE); set +a; \
	curl -fsS "http://127.0.0.1:$${JAEGER_HEALTH_PORT:-13133}/status" | grep -q '"healthy":true' && echo "jaeger: OK" || { echo "jaeger: FALHOU"; exit 1; }

s3-smoke: ## Cria bucket, faz upload e descarrega por URL pré-assinado (SeaweedFS)
	@set -a; . ./$(ENV_FILE); set +a; python3 -I scripts/s3-smoke.py

gen: ## Regenera o código dos contratos (backend/gen, app/src/gen). O sqlc entra com o F1
	@test -x app/node_modules/.bin/protoc-gen-es || (cd app && pnpm install --frozen-lockfile)
	@find backend/gen app/src/gen -type f \( -name '*.pb.go' -o -name '*.connect.go' -o -name '*_pb.ts' \) -delete
	buf generate

check-tool-versions: ## Os geradores de mise.toml têm de coincidir com as bibliotecas de backend/go.mod
	python3 -I scripts/check-tool-versions.py

gen-check: check-tool-versions gen ## Falha se os geradores não coincidirem com go.mod ou o código gerado não estiver commitado
	@test -z "$$(git status --porcelain -- backend/gen app/src/gen)" || \
	  { git status --porcelain -- backend/gen app/src/gen; echo "generated code is stale: run make gen and commit"; exit 1; }

lint: lint-proto lint-go ## buf lint + buf format + golangci-lint (eslint e clippy entram com a app e o firmware)

lint-proto:
	buf lint
	buf format --diff --exit-code

lint-go:
	cd backend && golangci-lint run ./...

breaking: ## buf breaking contra o branch local main (no CI cria-se com git branch --force main origin/main)
	buf breaking --against '.git#branch=main'

test: test-scripts ## Testes do backend com -race e dos scripts (app e firmware entram mais tarde)
	cd backend && go test -race ./...

test-scripts: ## Testes dos scripts Python (Zitadel falso, sem Docker)
	python3 -I -m unittest discover -s scripts -p 'test_*.py'

app-typecheck: ## tsc --noEmit ao TypeScript gerado
	@test -x app/node_modules/.bin/tsc || (cd app && pnpm install --frozen-lockfile)
	cd app && pnpm exec tsc --noEmit

secrets: ## gitleaks sobre o histórico completo do Git
	gitleaks git --config .gitleaks.toml --redact --no-banner

vuln: ## govulncheck ao backend (novas vulnerabilidades aparecem sem mudar código)
	cd backend && govulncheck ./...

workflows: ## actionlint + zizmor aos workflows (zizmor online se houver GH_TOKEN, senão offline)
	actionlint
	zizmor $(if $(GH_TOKEN),,--offline) .github/workflows

run-api: ## Corre o pitbox-api em 127.0.0.1:8081 com traces para o Jaeger local (make up); Ctrl+C para parar
	cd backend && PITBOX_ENV=local OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:$${OTLP_HTTP_PORT:-4318} go run ./cmd/api

zitadel-provision: ## Projeto, app OIDC, tempos dos tokens e política de login do Zitadel local (idempotente; precisa de make up)
	python3 -I scripts/zitadel-provision.py $(ARGS)

smoke-api: ## Prova ponta a ponta: /healthz, trace no Jaeger e paragem limpa (precisa de make up)
	bash scripts/api-smoke.sh

db-validate: ## Atlas: checksum (atlas.sum) e sintaxe das migrações
	atlas migrate validate --env local

db-lint: ## Migrações novas (vs main) sem DROP/TRUNCATE/RENAME/ALTER TYPE/SET NOT NULL (sem `-- lint:allow`)
	python3 -I scripts/db-lint.py main

db-test: ## Contentores descartáveis: migrações (Atlas) + schema.sql + testes SQL + teste de desvio
	bash scripts/db-test.sh

db-new: ## Nova migração vazia: make db-new name=<nome>; escreve o SQL e edita também schema.sql
	@test -n "$(name)" || { echo "uso: make db-new name=<nome>"; exit 1; }
	atlas migrate new $(name) --env local

db-migrate: ## Aplica as migrações ao Postgres do compose (make up) como pitbox_migrator; não apaga nada
	@test -f $(ENV_FILE) || { echo "Falta $(ENV_FILE): corre 'make init'."; exit 1; }
	@ok=$$($(COMPOSE) exec -T postgres psql -X -At -U postgres -d pitbox -c "SELECT (SELECT rolcanlogin FROM pg_roles WHERE rolname='pitbox_owner') AND 'role=pitbox_owner' = ANY (SELECT unnest(rolconfig) FROM pg_roles WHERE rolname='pitbox_migrator') AND (SELECT count(*) FROM pg_extension WHERE extname IN ('citext','btree_gist','timescaledb')) = 3" 2>/dev/null); \
	if [ "$$ok" != "t" ]; then \
	  echo "O bootstrap deste volume é anterior ao F0-06 (falta pitbox_owner com LOGIN, o 'role' do pitbox_migrator ou extensões)."; \
	  echo "Opção segura: 'make reset' (apaga os volumes, incluindo o Zitadel). Nada foi alterado."; exit 1; \
	fi
	@set -a; . ./$(ENV_FILE); set +a; \
	DATABASE_URL="postgres://pitbox_migrator:$$PITBOX_MIGRATOR_PASSWORD@127.0.0.1:$${POSTGRES_PORT:-5432}/pitbox?sslmode=disable" \
	atlas migrate apply --env local --allow-dirty

ci: lint test gen-check app-typecheck secrets vuln workflows db-test ## Corre localmente o mesmo que o CI

hooks: ## Ativa os hooks de .githooks (pre-push: gitleaks antes de cada push)
	git config core.hooksPath .githooks
