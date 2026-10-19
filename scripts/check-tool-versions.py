#!/usr/bin/env python3
"""Verifica que os geradores fixados em mise.toml coincidem com as bibliotecas de backend/go.mod.

O código gerado depende da versão do gerador e da biblioteca de runtime em simultâneo; se o
Renovate atualizar só um dos lados, o `make gen-check` apanharia no melhor caso um diff
confuso. Esta verificação falha cedo e diz o que alinhar.

Uso: python3 -I scripts/check-tool-versions.py [raiz]   (raiz por omissão: diretório do repositório)
"""
import re
import sys
import tomllib
from pathlib import Path

# (ferramenta em mise.toml, módulo em go.mod)
PAIRS = [
    ("protoc-gen-go", "google.golang.org/protobuf"),
    ("go:connectrpc.com/connect/v2/cmd/protoc-gen-connect-go", "connectrpc.com/connect/v2"),
    # `sqlc` is a Go tool in backend/go.mod (directive `tool`); the mise install is
    # only for interactive use. The two must be the same version.
    ("github:sqlc-dev/sqlc", "github.com/sqlc-dev/sqlc"),
]


def go_mod_versions(text: str) -> dict[str, str]:
    """module -> version, de todas as linhas `require` (em bloco ou soltas)."""
    versions = {}
    for line in text.splitlines():
        m = re.match(r"^\s*(?:require\s+)?([\w./~-]+)\s+(v[\w.+-]+)(?:\s*//.*)?$", line)
        if m:
            versions[m.group(1)] = m.group(2)
    return versions


def main() -> int:
    root = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(__file__).resolve().parent.parent
    tools = tomllib.loads((root / "mise.toml").read_text())["tools"]
    required = go_mod_versions((root / "backend/go.mod").read_text())

    problems = []
    for tool, module in PAIRS:
        spec = tools.get(tool)
        if isinstance(spec, dict):
            spec = spec.get("version")
        want = required.get(module)
        if spec is None:
            problems.append(f"mise.toml não declara {tool}")
        elif want is None:
            problems.append(f"backend/go.mod não exige {module}")
        elif want.removeprefix("v") != str(spec).removeprefix("v"):
            problems.append(f"{tool} = {spec} em mise.toml, mas {module} = {want} em backend/go.mod: alinhar os dois")
    for p in problems:
        print(f"check-tool-versions: {p}", file=sys.stderr)
    if not problems:
        print(f"check-tool-versions: {len(PAIRS)} pares alinhados")
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
