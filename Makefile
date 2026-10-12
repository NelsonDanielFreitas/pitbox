.DEFAULT_GOAL := help
.PHONY: help tools-check init up down reset ps logs health s3-smoke gen gen-check lint lint-proto lint-go breaking test app-typecheck secrets vuln workflows ci hooks

COMPOSE_DIR := deploy/compose
ENV_FILE    := $(COMPOSE_DIR)/.env
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

init: ## Gera deploy/compose/.env com segredos aleatórios (não sobrescreve)
	@if [ -e $(ENV_FILE) ]; then echo "$(ENV_FILE) já existe: não sobrescrevo (apaga-o para regenerar)."; exit 0; fi; \
	umask 077; \
	{ \
	  echo "POSTGRES_SUPERUSER_PASSWORD=$$(openssl rand -hex 24)"; \
	  echo "PITBOX_MIGRATOR_PASSWORD=$$(openssl rand -hex 24)"; \
	  echo "PITBOX_API_PASSWORD=$$(openssl rand -hex 24)"; \
	  echo "ZITADEL_DB_PASSWORD=$$(openssl rand -hex 24)"; \
	  echo "OPENFGA_DB_PASSWORD=$$(openssl rand -hex 24)"; \
	  echo "OPENFGA_PRESHARED_KEY=$$(openssl rand -hex 24)"; \
	  echo "VALKEY_PASSWORD=$$(openssl rand -hex 24)"; \
	  echo "ZITADEL_MASTERKEY=$$(openssl rand -hex 16)"; \
	  echo "ZITADEL_LOGIN_COOKIE_SECRET=$$(openssl rand -hex 32)"; \
	  echo "ZITADEL_ADMIN_USERNAME=zitadel-admin"; \
	  echo "ZITADEL_ADMIN_PASSWORD=$$(openssl rand -hex 12)A1!"; \
	  echo "S3_ACCESS_KEY=$$(openssl rand -hex 10)"; \
	  echo "S3_SECRET_KEY=$$(openssl rand -hex 24)"; \
	} > $(ENV_FILE); \
	chmod 600 $(ENV_FILE); \
	echo "Criado $(ENV_FILE) (permissões 600, fora do Git)."

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

gen-check: gen ## Falha se o código gerado não estiver commitado (apanha também ficheiros novos)
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

test: ## Testes do backend com -race (app e firmware entram mais tarde)
	cd backend && go test -race ./...

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

ci: lint test gen-check app-typecheck secrets vuln workflows ## Corre localmente o mesmo que o CI

hooks: ## Ativa os hooks de .githooks (pre-push: gitleaks antes de cada push)
	git config core.hooksPath .githooks
