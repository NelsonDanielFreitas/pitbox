#!/usr/bin/env python3
"""Provisiona o Zitadel local para o login do PitBox (F1-03, ADR 0013). Idempotente.

Leva a instância ao estado desejado, sempre pela mesma ordem:
  1. chave de assinatura ativa ECDSA P-256 (ES256); só cria e ativa uma se a ativa não for;
  2. tempos de vida dos tokens da instância (access 10 min, ID token 5 min, refresh 168 h/720 h);
  3. política de login sem segundo fator por SMS nem por email (ambos dão amr "mfa" e
     estragariam o step-up do F1-03c);
  4. projeto "pitbox";
  5. aplicação OIDC web "pitbox-api" (cliente confidencial, code + PKCE, access token opaco).
Escreve PITBOX_OIDC_CLIENT_ID e PITBOX_OIDC_CLIENT_SECRET em deploy/compose/.env (0600).
O que escreve ali nunca vai para o stdout, e o PAT nunca é impresso nem escrito em disco.

O PAT do utilizador "provisioner" (IAM_OWNER, 30 dias) vive num volume que só o contentor
zitadel-api monta; o script lê-o diretamente de lá (docker cp) para memória. Só local:
recusa falar com um endereço que não seja loopback.

Uso: python3 -I scripts/zitadel-provision.py [--rotate-secret] [--pat-file F] [--base-url U]
     python3 -I scripts/zitadel-provision.py --help
Só usa a biblioteca padrão.
"""
import argparse
import io
import json
import os
import re
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
ENV_FILE = ROOT / "deploy" / "compose" / ".env"
PAT_IN_CONTAINER = "/zitadel/provisioner/provisioner.pat"

PROJECT_NAME = "pitbox"
APP_NAME = "pitbox-api"

# Tempos de vida desejados, em segundos (têm de coincidir com o docker-compose.yml).
OIDC_SETTINGS = {
    "accessTokenLifetime": 10 * 60,
    "idTokenLifetime": 5 * 60,
    "refreshTokenIdleExpiration": 168 * 3600,
    "refreshTokenExpiration": 720 * 3600,
}
FORBIDDEN_SECOND_FACTORS = ("SECOND_FACTOR_TYPE_OTP_SMS", "SECOND_FACTOR_TYPE_OTP_EMAIL")

# O JSON do Connect/gRPC-gateway omite o valor zero de cada enum: lê-se a ausência assim.
ENUM_ZERO = {
    "applicationType": "OIDC_APP_TYPE_WEB",
    "authMethodType": "OIDC_AUTH_METHOD_TYPE_BASIC",
    "version": "OIDC_VERSION_1_0",
    "accessTokenType": "OIDC_TOKEN_TYPE_BEARER",
}
SET_FIELDS = ("redirectUris", "postLogoutRedirectUris", "responseTypes", "grantTypes")


class ProvisionError(Exception):
    """Falha esperada, com mensagem para o utilizador (nunca com segredos)."""


# --------------------------------------------------------------------------- puro


def desired_oidc_config(public_url: str) -> dict:
    """Configuração OIDC desejada da aplicação, a partir do URL público do pitbox-api."""
    base = public_url.rstrip("/")
    origins = [base]
    # The app dev server and the direct BFF are both used locally. Registering
    # both avoids re-provisioning when switching between `make run-api` (8081)
    # and `make app-dev` (5173).
    if base.startswith("http://localhost") or base.startswith("http://127.0.0.1"):
        origins = ["http://localhost:8081", "http://localhost:5173"]
    return {
        "redirectUris": [f"{origin}/auth/callback" for origin in origins],
        "postLogoutRedirectUris": [f"{origin}/auth/signed-out" for origin in origins],
        "backChannelLogoutUri": f"{base}/auth/backchannel-logout",
        "responseTypes": ["OIDC_RESPONSE_TYPE_CODE"],
        "grantTypes": ["OIDC_GRANT_TYPE_AUTHORIZATION_CODE", "OIDC_GRANT_TYPE_REFRESH_TOKEN"],
        "applicationType": "OIDC_APP_TYPE_WEB",
        "authMethodType": "OIDC_AUTH_METHOD_TYPE_BASIC",  # cliente confidencial + PKCE
        "version": "OIDC_VERSION_1_0",
        # Opaco: o BFF nunca lê o access token (só o usa no userinfo). Um JWT convidaria a confundi-lo
        # com o ID token (mesma chave, mesma aud); essa decisão é do F2-06, quando houver Bearer.
        "accessTokenType": "OIDC_TOKEN_TYPE_BEARER",
        "accessTokenRoleAssertion": False,
        "idTokenRoleAssertion": False,
        # O ID token não leva email nem nome: vai no id_token_hint do redirect de logout (histórico do
        # browser, logs). O servidor obtém-nos do endpoint userinfo com o access token (ADR 0013).
        "idTokenUserinfoAssertion": False,
        # http só é aceite em modo de desenvolvimento; um URL https não precisa dele.
        "developmentMode": base.startswith("http://"),
    }


def config_drift(current: dict, desired: dict) -> list:
    """Nomes dos campos em que a configuração atual difere da desejada."""
    drift = []
    for key, want in desired.items():
        have = current.get(key, ENUM_ZERO.get(key, False if isinstance(want, bool) else None))
        if key in SET_FIELDS:
            same = sorted(have or []) == sorted(want)
        else:
            same = have == want
        if not same:
            drift.append(key)
    return drift


def parse_duration(value) -> int:
    """'600s' (formato JSON de uma Duration) -> 600."""
    m = re.fullmatch(r"(\d+)(?:\.0+)?s", str(value))
    if not m:
        raise ProvisionError(f"duração inesperada na resposta do Zitadel: {value!r}")
    return int(m.group(1))


def parse_env(text: str) -> dict:
    out = {}
    for line in text.splitlines():
        m = re.match(r"([A-Za-z_][A-Za-z0-9_]*)=(.*)", line)
        if m:
            out[m.group(1)] = m.group(2)
    return out


def update_env(text: str, values: dict) -> str:
    """Define as chaves em `values` e deixa todo o resto do ficheiro exatamente como estava."""
    lines = text.splitlines()
    pending = dict(values)
    for i, line in enumerate(lines):
        m = re.match(r"([A-Za-z_][A-Za-z0-9_]*)=", line)
        if m and m.group(1) in pending:
            lines[i] = f"{m.group(1)}={pending.pop(m.group(1))}"
    lines += [f"{k}={v}" for k, v in pending.items()]
    return "\n".join(lines) + "\n"


def write_private(path: Path, content: str) -> None:
    """Escrita atómica com permissões 0600 desde a criação (sem janela legível por outros)."""
    fd, tmp = tempfile.mkstemp(dir=path.parent, prefix=path.name + ".")
    try:
        os.fchmod(fd, 0o600)
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            f.write(content)
        os.replace(tmp, path)
    except BaseException:
        Path(tmp).unlink(missing_ok=True)
        raise


# ------------------------------------------------------------------------- cliente


class _NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):  # o PAT nunca segue um redirect
        return None


class Zitadel:
    def __init__(self, base_url: str, pat: str):
        host = urllib.parse.urlparse(base_url).hostname
        if host not in ("localhost", "127.0.0.1", "::1"):
            raise ProvisionError(f"recuso falar com {host!r}: este script é só para o Zitadel local")
        self.base = base_url.rstrip("/")
        self._pat = pat
        self._opener = urllib.request.build_opener(_NoRedirect)

    def call(self, path: str, body=None, method: str = "POST"):
        data = None if method in ("GET", "DELETE") else json.dumps(body if body is not None else {}).encode()
        req = urllib.request.Request(
            self.base + path,
            data=data,
            method=method,
            headers={
                "Authorization": f"Bearer {self._pat}",
                "Content-Type": "application/json",
                "Accept": "application/json",
                "Connect-Protocol-Version": "1",
            },
        )
        try:
            with self._opener.open(req, timeout=15) as resp:
                raw = resp.read()
        except urllib.error.HTTPError as e:
            detail = e.read().decode("utf-8", "replace")[:300]
            raise ProvisionError(f"{method} {path}: HTTP {e.code} {detail}") from None
        except urllib.error.URLError as e:
            raise ProvisionError(f"{method} {path}: {e.reason}") from None
        return json.loads(raw) if raw else {}


def read_pat_from_container(path: str = PAT_IN_CONTAINER) -> str:
    """Lê um PAT de um ficheiro do contentor zitadel-api para memória (docker cp para stdout, formato tar)."""
    ids = subprocess.run(
        ["docker", "ps", "-q", "--filter", "label=com.docker.compose.project=pitbox",
         "--filter", "label=com.docker.compose.service=zitadel-api"],
        capture_output=True, text=True, check=False,
    ).stdout.split()
    if len(ids) != 1:
        raise ProvisionError("contentor zitadel-api não encontrado (corre 'make up')")
    cp = subprocess.run(["docker", "cp", f"{ids[0]}:{path}", "-"], capture_output=True, check=False)
    if cp.returncode != 0:
        raise ProvisionError(
            "não há PAT do utilizador de provisionamento: o Zitadel só o cria no primeiro arranque "
            "(volume vazio). Corre 'make reset' (apaga os dados locais)."
        )
    with tarfile.open(fileobj=io.BytesIO(cp.stdout)) as tar:
        member = next((m for m in tar.getmembers() if m.isfile()), None)
        if member is None:
            raise ProvisionError("o ficheiro do PAT está vazio")
        return tar.extractfile(member).read().decode().strip()


def read_pat_file(path: str) -> str:
    p = Path(path)
    try:
        if p.stat().st_mode & 0o077:
            raise ProvisionError(f"{p} tem permissões a mais (tem de ser 0600)")
        return p.read_text(encoding="utf-8").strip()
    except OSError as e:
        raise ProvisionError(f"não consigo ler {p}: {e.strerror}") from None


# ------------------------------------------------------------------------- passos


def ensure_signing_key(z: Zitadel, log) -> None:
    keys = z.call("/zitadel.webkey.v2.WebKeyService/ListWebKeys", {}).get("webKeys", [])
    active = next((k for k in keys if k.get("state") == "STATE_ACTIVE"), None)
    ecdsa = active.get("ecdsa") if active else None
    if ecdsa is not None and ecdsa.get("curve", "ECDSA_CURVE_P256") == "ECDSA_CURVE_P256":
        log("chave de assinatura: ES256 já ativa")
        return
    # Cuidado numa instância com clientes: o JWKS é servido com uma cache (max-age de 5 min por
    # omissão, OIDC.JWKSCacheControlMaxAge nos defaults do Zitadel) e a documentação do
    # ActivateWebKey desaconselha ativar uma chave criada dentro dessa janela, porque os clientes
    # podem ainda não a ter no JWKS e rejeitariam os tokens novos. Entre CreateWebKey e
    # ActivateWebKey tem de passar mais do que esse max-age. Aqui (instância local acabada de criar,
    # sem clientes) ativa-se logo.
    created = z.call("/zitadel.webkey.v2.WebKeyService/CreateWebKey", {"ecdsa": {"curve": "ECDSA_CURVE_P256"}})
    z.call("/zitadel.webkey.v2.WebKeyService/ActivateWebKey", {"id": created["id"]})
    log("chave de assinatura: criada e ativada ES256 (o JWKS pode demorar até 5 min nas caches)")


def ensure_oidc_settings(z: Zitadel, log) -> None:
    try:
        current = z.call("/admin/v1/settings/oidc", method="GET").get("settings", {})
        exists = True
    except ProvisionError as e:
        if "HTTP 404" not in str(e):
            raise
        current, exists = {}, False
    want = {k: f"{v}s" for k, v in OIDC_SETTINGS.items()}
    have = {k: parse_duration(current[k]) for k in OIDC_SETTINGS if k in current}
    if exists and have == OIDC_SETTINGS:
        log("tempos dos tokens: já corretos")
        return
    z.call("/admin/v1/settings/oidc", want, method="PUT" if exists else "POST")
    log("tempos dos tokens: atualizados")


def ensure_login_policy(z: Zitadel, log) -> None:
    present = z.call("/admin/v1/policies/login/second_factors/_search", {}).get("result", [])
    removed = [t for t in FORBIDDEN_SECOND_FACTORS if t in present]
    for t in removed:
        z.call(f"/admin/v1/policies/login/second_factors/{t}", method="DELETE")
    log(f"política de login: removidos {', '.join(removed)}" if removed else "política de login: sem OTP por SMS nem email")


def ensure_project(z: Zitadel, log) -> str:
    org = z.call("/management/v1/orgs/me", method="GET")["org"]["id"]
    found = z.call(
        "/zitadel.project.v2.ProjectService/ListProjects",
        {"filters": [{"projectNameFilter": {"projectName": PROJECT_NAME}}]},
    ).get("projects", [])
    match = [p for p in found if p.get("name") == PROJECT_NAME]
    if match:
        log("projeto: já existe")
        return match[0]["projectId"]
    created = z.call("/zitadel.project.v2.ProjectService/CreateProject", {"organizationId": org, "name": PROJECT_NAME})
    log("projeto: criado")
    return created["projectId"]


def ensure_app(z: Zitadel, project_id: str, public_url: str, env: dict, rotate: bool, log) -> dict:
    """Devolve as chaves de .env a escrever (vazio se nada mudou)."""
    desired = desired_oidc_config(public_url)
    apps = z.call(
        "/zitadel.application.v2.ApplicationService/ListApplications",
        {"filters": [{"projectIdFilter": {"projectId": project_id}}]},
    ).get("applications", [])
    app = next((a for a in apps if a.get("name") == APP_NAME), None)

    if app is None:
        created = z.call(
            "/zitadel.application.v2.ApplicationService/CreateApplication",
            {"projectId": project_id, "name": APP_NAME, "oidcConfiguration": desired},
        )
        cfg = created.get("oidcConfiguration", {})
        if not cfg.get("clientId") or not cfg.get("clientSecret"):
            raise ProvisionError("o Zitadel criou a aplicação sem devolver o client_id e o segredo")
        log("aplicação OIDC: criada")
        return {"PITBOX_OIDC_CLIENT_ID": cfg["clientId"], "PITBOX_OIDC_CLIENT_SECRET": cfg["clientSecret"]}

    cfg = app.get("oidcConfiguration", {})
    drift = config_drift(cfg, desired)
    if drift:
        z.call(
            "/zitadel.application.v2.ApplicationService/UpdateApplication",
            {"projectId": project_id, "applicationId": app["applicationId"], "oidcConfiguration": desired},
        )
        log(f"aplicação OIDC: corrigida ({', '.join(drift)})")
    else:
        log("aplicação OIDC: já correta")

    out = {}
    if env.get("PITBOX_OIDC_CLIENT_ID") != cfg.get("clientId"):
        out["PITBOX_OIDC_CLIENT_ID"] = cfg["clientId"]
    if rotate or not env.get("PITBOX_OIDC_CLIENT_SECRET"):
        secret = z.call(
            "/zitadel.application.v2.ApplicationService/GenerateClientSecret",
            {"projectId": project_id, "applicationId": app["applicationId"]},
        )["clientSecret"]
        out["PITBOX_OIDC_CLIENT_SECRET"] = secret
        log("segredo do cliente: gerado de novo")
    return out


def wait_ready(z: Zitadel) -> None:
    for _ in range(30):
        try:
            urllib.request.urlopen(z.base + "/.well-known/openid-configuration", timeout=3).close()
            return
        except OSError:
            time.sleep(2)
    raise ProvisionError(f"o Zitadel não responde em {z.base} (corre 'make up')")


def run(base_url: str, public_url: str, pat: str, env_file: Path, rotate: bool, log=print) -> None:
    z = Zitadel(base_url, pat)
    wait_ready(z)
    env_text = env_file.read_text(encoding="utf-8") if env_file.exists() else ""
    env = parse_env(env_text)
    ensure_signing_key(z, log)
    ensure_oidc_settings(z, log)
    ensure_login_policy(z, log)
    project_id = ensure_project(z, log)
    changes = ensure_app(z, project_id, public_url, env, rotate, log)
    if changes:
        write_private(env_file, update_env(env_text, changes))
        log(f"escrito em {env_file.name}: {', '.join(sorted(changes))}")


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description="Provisiona o Zitadel local para o login do PitBox.")
    ap.add_argument("--base-url", default=None, help="por omissão http://localhost:$ZITADEL_PORT (8080)")
    ap.add_argument("--public-url", default=None, help="URL público do pitbox-api (por omissão http://localhost:8081)")
    ap.add_argument("--pat-file", default=None, help="ficheiro 0600 com o PAT (por omissão lê-o do contentor)")
    ap.add_argument("--rotate-secret", action="store_true", help="gera um segredo de cliente novo")
    args = ap.parse_args(argv)
    env = parse_env(ENV_FILE.read_text(encoding="utf-8")) if ENV_FILE.exists() else {}
    base = args.base_url or f"http://localhost:{env.get('ZITADEL_PORT', '8080')}"
    public = args.public_url or env.get("PITBOX_PUBLIC_URL", "http://localhost:8081")
    try:
        pat = read_pat_file(args.pat_file) if args.pat_file else read_pat_from_container()
        run(base, public, pat, ENV_FILE, args.rotate_secret)
    except ProvisionError as e:
        print(f"zitadel-provision: {e}", file=sys.stderr)
        return 1
    print("zitadel-provision: OK")
    return 0


if __name__ == "__main__":
    sys.exit(main())
