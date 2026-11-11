#!/usr/bin/env python3
"""Gera deploy/compose/zitadel/roles.yaml: as permissões por papel do Zitadel + o papel mínimo da recuperação.

Porquê (#158, ADR 0034): nenhum papel embutido do Zitadel é estreito. Quem pode criar o link de registo
de passkey com os papéis embutidos (IAM_LOGIN_CLIENT, IAM_USER_MANAGER) também pode bloquear utilizadores
e muito mais. Define-se um papel próprio, IAM_PITBOX_ACCOUNT_RECOVERY, só com `user.read` e
`user.passkey.write`.

O Zitadel só aceita a lista INTEIRA de papéis (uma lista dada por ficheiro ou ambiente substitui a
lista por omissão): este script copia, sem alterar, a lista por omissão da versão fixada no
docker-compose.yml e acrescenta o papel novo. Ao subir a versão do Zitadel, volta a correr-se.

Uso: python3 -I scripts/zitadel-roles.py            # descarrega o defaults.yaml da versão fixada e escreve o ficheiro
     python3 -I scripts/zitadel-roles.py --check    # sai com 1 se o ficheiro não corresponde (não escreve)
     python3 -I scripts/zitadel-roles.py --defaults FICHEIRO   # usa um defaults.yaml local (testes)
Só usa a biblioteca padrão.
"""
import argparse
import hashlib
import re
import sys
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
COMPOSE = ROOT / "deploy" / "compose" / "docker-compose.yml"
OUT = ROOT / "deploy" / "compose" / "zitadel" / "roles.yaml"

RECOVERY_ROLE = "IAM_PITBOX_ACCOUNT_RECOVERY"
# Só o que a recuperação precisa: ler o utilizador (e os seus métodos de autenticação) e criar o
# link de registo de passkey. Sem `user.write`, `user.credential.write`, `user.delete`, sessões, etc.
RECOVERY_PERMISSIONS = ("user.read", "user.passkey.write")

DEFAULTS_URL = "https://raw.githubusercontent.com/zitadel/zitadel/{tag}/cmd/defaults.yaml"
_TAG = re.compile(r"image:\s*ghcr\.io/zitadel/zitadel:(v\d+\.\d+\.\d+)@sha256:[0-9a-f]{64}")


class RolesError(Exception):
    pass


def pinned_tag(compose_text: str) -> str:
    m = _TAG.search(compose_text)
    if not m:
        raise RolesError("não encontrei a imagem do Zitadel (com tag e digest) no docker-compose.yml")
    return m.group(1)


def internal_mappings(defaults_text: str) -> list:
    """As linhas de InternalAuthZ.RolePermissionMappings (sem a chave), tal como estão."""
    lines = defaults_text.splitlines()
    try:
        start = lines.index("InternalAuthZ:")
        key = next(i for i in range(start, len(lines)) if lines[i] == "  RolePermissionMappings:")
        end = next(i for i in range(key + 1, len(lines)) if re.match(r"^\S", lines[i]))
    except (ValueError, StopIteration):
        raise RolesError("InternalAuthZ.RolePermissionMappings não encontrado no defaults.yaml") from None
    block = [ln for ln in lines[key + 1:end]]
    while block and not block[-1].strip():
        block.pop()
    if not block or not block[0].startswith("    - Role: "):
        raise RolesError("formato inesperado do bloco de papéis")
    return block


def role_names(block: list) -> list:
    return [m.group(1) for ln in block if (m := re.match(r'^    - Role: "([A-Z_]+)"$', ln))]


def render(defaults_text: str, tag: str) -> str:
    block = internal_mappings(defaults_text)
    names = role_names(block)
    if RECOVERY_ROLE in names:
        raise RolesError(f"o Zitadel já define um papel {RECOVERY_ROLE}")
    if "IAM_OWNER" not in names or "IAM_LOGIN_CLIENT" not in names:
        raise RolesError("a lista por omissão não tem os papéis esperados; recuso gerar uma lista incompleta")
    digest = hashlib.sha256("\n".join(block).encode()).hexdigest()
    custom = [f'    - Role: "{RECOVERY_ROLE}"', "      Permissions:"] + [f'        - "{p}"' for p in RECOVERY_PERMISSIONS]
    header = [
        "# GERADO por scripts/zitadel-roles.py — não editar à mão (#158, ADR 0034).",
        f"# Origem: lista por omissão do Zitadel {tag} (sha256 do bloco copiado: {digest}),",
        f"# mais o papel {RECOVERY_ROLE} no fim. Aplica-se no `zitadel setup` (arranque do contentor).",
        "InternalAuthZ:",
        "  RolePermissionMappings:",
    ]
    return "\n".join(header + block + custom) + "\n"


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--check", action="store_true")
    ap.add_argument("--defaults", type=Path, default=None)
    args = ap.parse_args(argv)
    try:
        tag = pinned_tag(COMPOSE.read_text(encoding="utf-8"))
        if args.defaults:
            text = args.defaults.read_text(encoding="utf-8")
        else:
            with urllib.request.urlopen(DEFAULTS_URL.format(tag=tag), timeout=30) as r:  # noqa: S310 (https fixo)
                text = r.read(4 << 20).decode("utf-8")
        wanted = render(text, tag)
    except (RolesError, OSError) as e:
        print(f"zitadel-roles: {e}", file=sys.stderr)
        return 1
    if args.check:
        if not OUT.exists() or OUT.read_text(encoding="utf-8") != wanted:
            print("zitadel-roles: roles.yaml desatualizado; corre scripts/zitadel-roles.py", file=sys.stderr)
            return 1
        return 0
    OUT.write_text(wanted, encoding="utf-8")
    print(f"zitadel-roles: escrito {OUT.relative_to(ROOT)} ({len(role_names(internal_mappings(text)))} papéis + {RECOVERY_ROLE})")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
