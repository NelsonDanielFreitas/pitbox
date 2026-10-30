.DEFAULT_GOAL := help
.PHONY: help tools-check init up down reset ps logs health s3-smoke gen gen-check sqlc-check check-tool-versions lint lint-proto lint-go lint-app breaking test test-app fuzz app-install app-dev app-lint app-typecheck app-test app-build app-e2e secrets vuln workflows ci hooks db-validate db-lint db-test db-new db-migrate run-api run-worker smoke-api smoke-auth zitadel-provision test-scripts authz-test

COMPOSE_DIR := deploy/compose
ENV_FILE    := $(COMPOSE_DIR)/.env
# Data de validade do PAT do utilizador de provisionamento do Zitadel (30 dias). Só conta no
# primeiro arranque do volume; o compose exige-a, por isso passa sempre pelo make.
# := e não ?=: com export, o $(shell ...) de um ?= volta a expandir a variável exportada dentro
# do próprio shell (recursão); com nomes longos isso estoura a stack do make (SIGSEGV no push).
ifeq ($(origin ZITADEL_PROVISIONER_PAT_EXPIRATION), undefined)
ZITADEL_PROVISIONER_PAT_EXPIRATION := $(shell date -u -d '+30 days' +%Y-%m-%dT%H:%M:%SZ)
endif
export ZITADEL_PROVISIONER_PAT_EXPIRATION
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
	add PITBOX_AUTH_KEYS "k1:$$(openssl rand -base64 32)"; \
	add PITBOX_SYNC_PASS_KEYS "k1:$$(openssl rand -base64 32)"; \
	add MAILPIT_SMTP_USERNAME "pitbox-smtp"; \
	add MAILPIT_SMTP_PASSWORD "$$(openssl rand -hex 24)"; \
	add MAILPIT_UI_USERNAME "pitbox-ui"; \
	add MAILPIT_UI_PASSWORD "$$(openssl rand -hex 24)"; \
	echo "$(ENV_FILE): permissões 600, fora do Git."

up: ## Levanta todo o ambiente local e espera pelos health checks
	@test -f $(ENV_FILE) || { echo "Falta $(ENV_FILE): corre 'make init'."; exit 1; }
	$(COMPOSE) up -d --wait
	# traefik.yml é estático e bind-mounted; um up normal não o recarrega. Recria só o
	# proxy para aplicar access logs/definições estáticas, sem tocar nos dados.
	$(COMPOSE) up -d --wait --force-recreate zitadel-proxy

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

gen: ## Regenera o código dos contratos (backend/gen, app/src/gen) e o SQL (sqlc)
	@test -x app/node_modules/.bin/protoc-gen-es || (cd app && pnpm install --frozen-lockfile)
	@find backend/gen app/src/gen -type f \( -name '*.pb.go' -o -name '*.connect.go' -o -name '*_pb.ts' \) -delete
	buf generate
	@find backend/internal -path '*/sqlcgen/*' -type f -name '*.go' -delete
	cd backend && go tool sqlc generate

check-tool-versions: ## Os geradores de mise.toml têm de coincidir com as bibliotecas de backend/go.mod
	python3 -I scripts/check-tool-versions.py

gen-check: check-tool-versions gen ## Falha se os geradores não coincidirem com go.mod ou o código gerado não estiver commitado
	@test -z "$$(git status --porcelain -- backend/gen app/src/gen)" || \
	  { git status --porcelain -- backend/gen app/src/gen; echo "generated code is stale: run make gen and commit"; exit 1; }

sqlc-check: ## Falha se o SQL gerado (sqlc) não corresponder às queries e ao schema.sql
	cd backend && go tool sqlc generate
	@test -z "$$(git status --porcelain -- 'backend/internal/*/store/sqlcgen' 'backend/internal/platform/*/sqlcgen')" || \
	  { git status --porcelain -- 'backend/internal/*/store/sqlcgen' 'backend/internal/platform/*/sqlcgen'; echo "generated SQL is stale: run make sqlc-check and commit"; exit 1; }

lint: lint-proto lint-go app-lint ## buf lint + buf format + golangci-lint + eslint

lint-proto:
	buf lint
	buf format --diff --exit-code

lint-go:
	cd backend && golangci-lint run ./...

breaking: ## buf breaking contra o branch local main (no CI cria-se com git branch --force main origin/main)
	buf breaking --against '.git#branch=main'

test: test-scripts ## Testes do backend e dos scripts (app: make app-test)
	cd backend && go test -race ./...

test-app: app-test ## Alias para os testes da app

fuzz: ## Fuzzers do setup (1 min cada): validação, comparação e conversão de Struct
	cd backend && go test -run=^$$ -fuzz=FuzzValidateV1 -fuzztime=1m ./internal/setup/domain
	cd backend && go test -run=^$$ -fuzz=FuzzCompare -fuzztime=1m ./internal/setup/domain
	cd backend && go test -run=^$$ -fuzz=FuzzDataFromProto -fuzztime=1m ./internal/setup/transport

test-scripts: ## Testes dos scripts Python (Zitadel falso, sem Docker)
	python3 -I -m unittest discover -s scripts -p 'test_*.py'

app-install: ## Instala as dependências da app
	cd app && pnpm install --frozen-lockfile

app-dev: ## Corre o Vite em localhost:5173 (proxy para a API; não uses 127.0.0.1)
	cd app && pnpm dev

app-lint: ## ESLint da app
	cd app && pnpm lint

app-typecheck: ## tsc --noEmit da app
	@test -x app/node_modules/.bin/tsc || (cd app && pnpm install --frozen-lockfile)
	cd app && pnpm typecheck

app-test: ## Testes Vitest da app
	cd app && pnpm test

app-build: ## Build de produção da app
	cd app && pnpm build

app-e2e: app-build ## Playwright sobre o build de produção
	cd app && pnpm e2e

secrets: ## gitleaks sobre o histórico completo do Git
	gitleaks git --config .gitleaks.toml --redact --no-banner

vuln: ## govulncheck ao backend (novas vulnerabilidades aparecem sem mudar código)
	cd backend && govulncheck ./...

workflows: ## actionlint + zizmor aos workflows (zizmor online se houver GH_TOKEN, senão offline)
	actionlint
	zizmor $(if $(GH_TOKEN),,--offline) .github/workflows

run-api: ## Corre o pitbox-api em 127.0.0.1:8081 com traces para o Jaeger; com login se `make zitadel-provision` já correu (Ctrl+C para parar)
	@bash scripts/run-api.sh

run-worker: ## Corre o pitbox-worker (poller da outbox) contra a infraestrutura local (Ctrl+C para parar)
	@bash scripts/run-worker.sh

zitadel-provision: ## Projeto, app OIDC, tempos dos tokens e política de login do Zitadel local (idempotente; precisa de make up)
	python3 -I scripts/zitadel-provision.py $(ARGS)

openfga-provision: ## Cria a store e o modelo OpenFGA local e guarda os ids no .env (idempotente; precisa de make up)
	python3 -I scripts/openfga-provision.py

smoke-auth: ## Prova o login, registo e verificação contra o Zitadel/Mailpit locais (precisa de make up e make zitadel-provision)
	python3 -I scripts/auth-smoke.py

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

authz-test: ## Asserções do modelo OpenFGA
	fga model test --tests backend/authz/model.fga.yaml

ci: lint test gen-check sqlc-check app-install app-typecheck app-test app-build app-e2e secrets vuln workflows db-test authz-test ## Corre localmente o mesmo que o CI

hooks: ## Ativa os hooks de .githooks (pre-push: gitleaks antes de cada push)
	git config core.hooksPath .githooks
