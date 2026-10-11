.PHONY: tools-check

# Verifica que a toolchain declarada em mise.toml está instalada e ativa.
tools-check:
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
