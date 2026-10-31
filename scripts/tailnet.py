#!/usr/bin/env python3
"""Perfil «telemóvel» (ADR 0029, F1-14c): valida PITBOX_TAILNET_HOST e deriva dele o resto.

PITBOX_TAILNET_HOST é o nome da máquina na tailnet (ex.: portatil-wsl.tail1234.ts.net), sem
esquema, porta nem caminho. Sem ele, tudo fica como hoje (localhost). Uma única definição da
regra, usada pelo Makefile, pelo run-api.sh e pelo zitadel-provision.py.

Uso: python3 -I scripts/tailnet.py host            # valida o valor do .env; imprime-o (vazio se não houver)
     python3 -I scripts/tailnet.py check VALOR     # valida VALOR; sai com 1 se for inválido
     python3 -I scripts/tailnet.py compose-env     # linhas CHAVE=VALOR para o docker compose (vazio sem perfil)
Só usa a biblioteca padrão.
"""
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
ENV_FILE = ROOT / "deploy" / "compose" / ".env"

# Porta HTTPS do Zitadel na tailnet; a da app é a 443 (por omissão).
ZITADEL_TAILNET_PORT = 8443

_LABEL = r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?"
# Pelo menos um rótulo antes de «ts.net»; letras minúsculas, sem esquema, porta, caminho nem
# espaços. Rejeita «pc.ts.net.evil.example» (tem de acabar em .ts.net).
_HOST = re.compile(rf"^(?:{_LABEL}\.)+ts\.net$")


class TailnetError(Exception):
    pass


def validate_host(raw: str) -> str:
    """Devolve o host se for «<rótulos>.ts.net» em minúsculas; senão levanta TailnetError."""
    if not isinstance(raw, str) or len(raw) > 253 or not _HOST.fullmatch(raw):
        raise TailnetError(
            "PITBOX_TAILNET_HOST tem de ser só o nome da máquina em minúsculas, terminado em .ts.net "
            "(ex.: portatil-wsl.tail1234.ts.net), sem http://, porta nem caminho"
        )
    return raw


def read_host_from_env(env_file: Path = ENV_FILE) -> str:
    """O valor validado do .env, ou '' se não estiver definido."""
    if not env_file.exists():
        return ""
    for line in env_file.read_text(encoding="utf-8").splitlines():
        if line.startswith("PITBOX_TAILNET_HOST="):
            value = line.split("=", 1)[1].strip()
            return validate_host(value) if value else ""
    return ""


def compose_env(host: str) -> dict:
    """Variáveis que o docker-compose.yml lê para o Zitadel; vazio no perfil localhost."""
    if not host:
        return {}
    hostport = f"{host}:{ZITADEL_TAILNET_PORT}"
    return {
        "PITBOX_ZITADEL_DOMAIN": host,
        "PITBOX_ZITADEL_EXTERNAL_PORT": str(ZITADEL_TAILNET_PORT),
        "PITBOX_ZITADEL_EXTERNAL_SECURE": "true",
        "PITBOX_ZITADEL_ORIGIN": f"https://{hostport}",
        "PITBOX_ZITADEL_LOGIN_HOST": hostport,
        "PITBOX_ZITADEL_LOGIN_PROTO": "https",
    }


def main(argv=None) -> int:
    args = list(sys.argv[1:] if argv is None else argv)
    try:
        if args == ["host"]:
            print(read_host_from_env())
        elif len(args) == 2 and args[0] == "check":
            print(validate_host(args[1]))
        elif args == ["compose-env"]:
            for k, v in compose_env(read_host_from_env()).items():
                print(f"{k}={v}")
        else:
            print(__doc__, file=sys.stderr)
            return 2
    except TailnetError as e:
        print(f"tailnet: {e}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
