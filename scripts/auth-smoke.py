"""Prova o login do pitbox-api ponta a ponta contra o Zitadel local, sem browser (F1-03).

Faz o que o browser faria, passo a passo, e confirma cada passo:
  1. arranca o binário com a configuração local (precisa de `make up` e `make zitadel-provision`);
  2. cria um utilizador de teste no Zitadel (email verificado, palavra-passe aleatória);
  3. GET /auth/login -> redireciona para o Zitadel; segue o authorize até ao auth request;
  4. conclui o login do lado do Zitadel pela API de sessões (padrão das UIs de login próprias:
     criar sessão com utilizador + palavra-passe, e finalizar o auth request);
  5. entrega o callback à API: cria o utilizador no primeiro login e a sessão (cookie __Host-);
  6. /auth/session responde com o utilizador; um callback sem cookie e um com state errado dão 400;
  7. logout (POST com Origin) -> 200 com meta refresh para o end_session do Zitadel; /auth/session passa a 401;
  8. segundo login do mesmo utilizador -> o MESMO user_id (não duplica);
  9. refresh a sério: encurta o access token para 5 s no Zitadel, espera que expire e confirma que
     a API renova (a geração da sessão no Valkey sobe) sem pedir novo login; depois desativa o
     utilizador e confirma que o refresh seguinte falha e a sessão acaba (UNAUTHENTICATED);
 10. auditoria na base (login, logout, falhas, sessão revogada), nada de segredos nos logs, nada
     do valor do cookie no Valkey; repõe os tempos dos tokens e apaga os utilizadores de teste;
 11. registo sem email verificado, leitura do link no Mailpit, página `/confirm-email`, verificação
     do email e login; recuperação de palavra-passe; 10 falhas sem lockout; 11 POST ao login v2
     do mesmo IP -> 429; códigos fora dos access logs.

Usa o PAT do login-client (só sessões) para o login e o do provisionamento só para criar e apagar
o utilizador de teste. Nenhum PAT nem palavra-passe é impresso. Só biblioteca padrão.
"""
import base64
import html
import http.client
from email import policy
from email.parser import BytesParser
import importlib.util
import json
import os
import re
import secrets
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
from http.cookies import SimpleCookie
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
ENV_FILE = ROOT / "deploy" / "compose" / ".env"
API = "http://localhost:8081"  # onde o smoke fala com o pitbox-api (escuta só em 127.0.0.1)
# Perfil (ADR 0029): no localhost o URL público é a própria API e o issuer é o Zitadel local. No perfil
# telemóvel o URL público e o issuer são HTTPS da tailnet, que o PC não alcança: http_call() reencaminha-os
# para o endereço local (com o Host certo) e o API usa PITBOX_OIDC_LOCAL_DIAL, como o `make run-api`.
PUBLIC = API
ISSUER = ""  # preenchido em main() (depende da porta do Zitadel)
ZLOCAL = ""
ZHOST = ""   # Host que escolhe a instância do Zitadel; vazio no localhost

spec = importlib.util.spec_from_file_location("provision", ROOT / "scripts" / "zitadel-provision.py")
provision = importlib.util.module_from_spec(spec)
spec.loader.exec_module(provision)
_pk_spec = importlib.util.spec_from_file_location("softpasskey", ROOT / "scripts" / "softpasskey.py")
softpasskey = importlib.util.module_from_spec(_pk_spec)
_pk_spec.loader.exec_module(softpasskey)


SUBJECT_RE = re.compile(r"\d{1,32}")  # os ids do Zitadel (o `sub` do IdP) são numéricos


def psql(env: dict, query: str) -> str:
    """Corre uma consulta como superutilizador no Postgres local (só para o smoke, nunca em produção).

    A palavra-passe vai pelo ambiente do `docker` (`-e PGPASSWORD` sem valor), não na linha de
    comandos, para não aparecer na lista de processos.
    """
    return subprocess.run(
        ["docker", "exec", "-e", "PGPASSWORD", "pitbox-postgres-1", "psql", "-X", "-At", "-h", "127.0.0.1", "-U", "postgres", "-d", "pitbox", "-c", query],
        env={**os.environ, "PGPASSWORD": env["POSTGRES_SUPERUSER_PASSWORD"]},
        capture_output=True, text=True, check=True).stdout.strip()


def cleanup_user_accounts(run_sql, subjects) -> int:
    """Apaga de identity.user_account só as linhas dos utilizadores de teste, pelo idp_subject.

    Fail closed: sem ids não corre SQL (nunca um DELETE sem WHERE) e um id fora do formato
    numérico recusa tudo em vez de ser colado no SQL. Devolve quantas linhas apagou.
    """
    subjects = list(dict.fromkeys(subjects))
    if not subjects:
        return 0
    if not all(isinstance(x, str) and SUBJECT_RE.fullmatch(x) for x in subjects):
        raise ValueError("idp_subject inválido; nada foi apagado")
    in_list = ", ".join(f"'{x}'" for x in subjects)
    return int(run_sql(
        f"WITH d AS (DELETE FROM identity.user_account WHERE idp_subject IN ({in_list}) RETURNING 1) SELECT count(*) FROM d"))


def fail(msg: str) -> None:
    print(f"auth-smoke: FALHOU: {msg}", file=sys.stderr)
    raise SystemExit(1)


def check(cond: bool, msg: str) -> None:
    if not cond:
        fail(msg)
    print(f"  ok  {msg}")


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *a, **k):
        return None


OPENER = urllib.request.build_opener(NoRedirect)


def to_local(url, headers):
    """Reencaminha um URL público do perfil telemóvel para o endereço local (e põe o Host certo)."""
    headers = dict(headers or {})
    if ZHOST and url.startswith(ISSUER):
        headers["Host"] = ZHOST
        return ZLOCAL + url[len(ISSUER):], headers
    if PUBLIC != API and url.startswith(PUBLIC):
        return API + url[len(PUBLIC):], headers
    return url, headers


def http_call(method, url, headers=None, body=None):
    """Devolve (status, headers, corpo); nunca segue redirects."""
    url, headers = to_local(url, headers)
    req = urllib.request.Request(url, data=body, method=method, headers=headers or {})
    try:
        with OPENER.open(req, timeout=15) as r:
            return r.status, r.headers, r.read()
    except urllib.error.HTTPError as e:
        return e.code, e.headers, e.read()


def meta_refresh_target(body: str) -> str:
    """Extrai o URL de uma página 200 com meta refresh (logout/landing)."""
    m = re.search(r'http-equiv="refresh" content="0;url=([^"]+)"', body)
    if not m:
        fail("resposta sem meta refresh")
    return html.unescape(m.group(1))


def cookies_of(headers) -> dict:
    out = {}
    for raw in headers.get_all("Set-Cookie") or []:
        c = SimpleCookie()
        c.load(raw)
        for k, m in c.items():
            out[k] = (m.value, dict(m.items()))
    return out


def cookie_header(**kw) -> str:
    return "; ".join(f"{k}={v}" for k, v in kw.items())

class DeviceKey:
    """Chave de sessão do telemóvel (ECDSA P-256), feita com o openssl para manter só a biblioteca padrão."""

    def __init__(self, workdir: str):
        self.pem = os.path.join(workdir, "device-" + secrets.token_hex(4) + ".pem")
        subprocess.run(["openssl", "ecparam", "-genkey", "-name", "prime256v1", "-noout", "-out", self.pem], check=True, capture_output=True)
        der = subprocess.run(["openssl", "pkey", "-in", self.pem, "-pubout", "-outform", "DER"], check=True, capture_output=True).stdout
        self.spki = base64.b64encode(der).decode()

    def proof(self, domain: str, *fields: str) -> str:
        """Assinatura sobre os campos com prefixo de comprimento (igual a proofMessage em Go), em r||s base64url."""
        msg = b"".join(len(f).to_bytes(4, "big") + f for f in (x.encode() for x in (domain,) + fields))
        der = subprocess.run(["openssl", "dgst", "-sha256", "-sign", self.pem], input=msg, check=True, capture_output=True).stdout
        # DER: 30 len 02 rlen r 02 slen s
        i = 2 + (der[1] & 0x7F if der[1] & 0x80 else 0)
        rlen = der[i + 1]
        r = der[i + 2:i + 2 + rlen]
        j = i + 2 + rlen
        slen = der[j + 1]
        sv = der[j + 2:j + 2 + slen]
        raw = r.lstrip(b"\0").rjust(32, b"\0") + sv.lstrip(b"\0").rjust(32, b"\0")
        return base64.urlsafe_b64encode(raw).decode().rstrip("=")


def pkce_pair():
    verifier = secrets.token_urlsafe(48)
    import hashlib
    challenge = base64.urlsafe_b64encode(hashlib.sha256(verifier.encode()).digest()).decode().rstrip("=")
    return verifier, challenge


def post_json(url, body, headers=None):
    h = {"Content-Type": "application/json"}
    h.update(headers or {})
    status, hdrs, raw = http_call("POST", url, h, json.dumps(body).encode())
    try:
        return status, json.loads(raw or b"{}")
    except ValueError:
        return status, {}


def mailpit_headers(env: dict) -> dict:
    user = env.get("MAILPIT_UI_USERNAME", "")
    password = env.get("MAILPIT_UI_PASSWORD", "")
    if not user:
        return {}
    token = base64.b64encode(f"{user}:{password}".encode()).decode()
    return {"Authorization": f"Basic {token}"}


def mailpit_call(env: dict, method: str, path: str):
    port = env.get("MAILPIT_UI_PORT", "8025")
    return http_call(method, f"http://127.0.0.1:{port}{path}", mailpit_headers(env))


def clear_mailpit(env: dict) -> None:
    status, _, _ = mailpit_call(env, "DELETE", "/api/v1/messages")
    check(status in (200, 204), "Mailpit limpo")


def latest_mailpit_raw(env: dict) -> bytes:
    deadline = time.time() + 20
    while time.time() < deadline:
        status, _, body = mailpit_call(env, "GET", "/api/v1/messages?limit=50")
        if status == 200:
            data = json.loads(body)
            messages = data.get("messages", [])
            if messages:
                message_id = messages[0].get("ID") or messages[0].get("id")
                if message_id:
                    st, _, raw = mailpit_call(env, "GET", f"/api/v1/message/{message_id}/raw")
                    check(st == 200, "email lido do Mailpit")
                    return raw
        time.sleep(0.5)
    fail("o email não chegou ao Mailpit em 20 s")


def email_link(raw: bytes, suffix: str) -> str:
    msg = BytesParser(policy=policy.default).parsebytes(raw)
    pattern = re.compile(
        rf'https?://[^"\s<>]+/ui/v2/login/{suffix}\?[^"\s<>]+',
    )
    for part in msg.walk():
        if part.get_content_type() not in ("text/plain", "text/html"):
            continue
        try:
            text = part.get_content()
        except (LookupError, UnicodeDecodeError):
            continue
        match = pattern.search(html.unescape(text))
        if match:
            return match.group(0).rstrip(").,;")
    fail(f"email sem link {suffix}")


def verification_link(raw: bytes) -> str:
    return email_link(raw, "(?:mail/verification|verify)")




def main() -> int:
    if not ENV_FILE.exists():
        fail("falta deploy/compose/.env (make init)")
    env = provision.parse_env(ENV_FILE.read_text())
    for k in (
        "PITBOX_OIDC_CLIENT_ID", "PITBOX_OIDC_CLIENT_SECRET", "PITBOX_AUTH_KEYS",
        "VALKEY_API_PASSWORD", "PITBOX_API_PASSWORD",
        "PITBOX_OPENFGA_ADDR", "PITBOX_OPENFGA_PRESHARED_KEY",
        "PITBOX_OPENFGA_STORE_ID", "PITBOX_OPENFGA_MODEL_ID",
        "MAILPIT_SMTP_USERNAME", "MAILPIT_SMTP_PASSWORD",
        "MAILPIT_UI_USERNAME", "MAILPIT_UI_PASSWORD", "PITBOX_OIDC_MOBILE_CLIENT_ID",
        "PITBOX_ZITADEL_RECOVERY_PAT",
    ):
        if not env.get(k):
            fail(f"falta {k} no .env (make init; make zitadel-provision; make openfga-provision)")
    global PUBLIC, ISSUER, ZLOCAL, ZHOST
    zport = env.get("ZITADEL_PORT", "8080")
    ZLOCAL = f"http://localhost:{zport}"
    tailnet_host = provision.tailnet.read_host_from_env(ENV_FILE)
    if tailnet_host:
        PUBLIC = f"https://{tailnet_host}"
        ZHOST = f"{tailnet_host}:{provision.tailnet.ZITADEL_TAILNET_PORT}"
        ISSUER = f"https://{ZHOST}"
    else:
        ISSUER = ZLOCAL

    try:
        socket.create_connection(("127.0.0.1", 8081), timeout=0.5).close()
        fail("a porta 8081 já está ocupada (pára o make run-api)")
    except OSError:
        pass

    accounts_before = int(psql(env, "SELECT count(*) FROM identity.user_account"))

    work = tempfile.mkdtemp(prefix="pitbox-auth-smoke-")
    binary, logpath = os.path.join(work, "api"), os.path.join(work, "log")
    print("== 1. Compilar e arrancar o pitbox-api")
    subprocess.run(["go", "build", "-o", binary, "./cmd/api"], cwd=ROOT / "backend", check=True)
    child_env = {
        "PATH": os.environ["PATH"], "PITBOX_ENV": "local", "PITBOX_HTTP_ADDR": "127.0.0.1:8081", "PITBOX_PUBLIC_URL": PUBLIC,
        "PITBOX_OIDC_ISSUER": ISSUER, "PITBOX_OIDC_CLIENT_ID": env["PITBOX_OIDC_CLIENT_ID"],
        "PITBOX_OIDC_CLIENT_SECRET": env["PITBOX_OIDC_CLIENT_SECRET"], "PITBOX_AUTH_KEYS": env["PITBOX_AUTH_KEYS"],
        "PITBOX_VALKEY_ADDR": f"127.0.0.1:{env.get('VALKEY_PORT', '6379')}", "PITBOX_VALKEY_PASSWORD": env["VALKEY_API_PASSWORD"],
        "PITBOX_DATABASE_URL": f"postgres://pitbox_api:{env['PITBOX_API_PASSWORD']}@127.0.0.1:{env.get('POSTGRES_PORT', '5432')}/pitbox?sslmode=disable",
        "PITBOX_OPENFGA_ADDR": env["PITBOX_OPENFGA_ADDR"],
        "PITBOX_OPENFGA_PRESHARED_KEY": env["PITBOX_OPENFGA_PRESHARED_KEY"],
        "PITBOX_OPENFGA_STORE_ID": env["PITBOX_OPENFGA_STORE_ID"],
        "PITBOX_OPENFGA_MODEL_ID": env["PITBOX_OPENFGA_MODEL_ID"],
        "PITBOX_OIDC_MOBILE_CLIENT_ID": env["PITBOX_OIDC_MOBILE_CLIENT_ID"],
        "PITBOX_CORS_ORIGINS": env.get("PITBOX_CORS_ORIGINS", "https://localhost"),
        "PITBOX_ZITADEL_RECOVERY_PAT": env["PITBOX_ZITADEL_RECOVERY_PAT"],
    }
    if tailnet_host:
        child_env["PITBOX_OIDC_LOCAL_DIAL"] = f"127.0.0.1:{zport}"
        child_env["PITBOX_OIDC_LOCAL_LOGOUT_ISSUER"] = f"https://localhost:{provision.tailnet.ZITADEL_TAILNET_PORT}"
    logf = open(logpath, "w")
    proc = subprocess.Popen([binary], env=child_env, stdout=logf, stderr=subprocess.STDOUT)
    user_id = None
    shortened = False
    secrets_seen = []  # valores que nunca podem aparecer nos logs
    admin_pat = None
    try:
        for _ in range(50):
            try:
                if http_call("GET", API + "/healthz")[0] == 200:
                    break
            except OSError:
                time.sleep(0.2)
        else:
            fail("a API não ficou pronta (ver o log em " + logpath + ")")
        check(True, "pitbox-api a correr em " + API)

        admin_pat = provision.read_pat_from_container()
        login_pat = provision.read_pat_from_container("/zitadel/bootstrap/login-client.pat")
        admin = provision.Zitadel(ZLOCAL, admin_pat, ZHOST)
        login = provision.Zitadel(ZLOCAL, login_pat, ZHOST)

        print("== 2. Utilizador de teste no Zitadel")
        tag = secrets.token_hex(4)
        username, email = f"pitbox-smoke-{tag}", f"smoke-{tag}@example.eu"
        password = "Aa1!" + secrets.token_urlsafe(18)
        secrets_seen += [password]
        created = admin.call("/v2/users/human", {
            "username": username, "profile": {"givenName": "Smoke", "familyName": "Test"},
            "email": {"email": email, "isVerified": True}, "password": {"password": password, "changeRequired": False},
        })
        zitadel_user = created["userId"]
        test_users = [zitadel_user]
        login_name = admin.call(f"/v2/users/{zitadel_user}", method="GET")["user"]["preferredLoginName"]
        secrets_seen += [email]
        check(True, "utilizador criado (email verificado)")

        last = {}  # a sessão do Zitadel criada pelo último sign_in (para o passo do back-channel)

        def sign_in(as_login_name=None, as_password=None):
            current_login = as_login_name or login_name
            current_password = as_password or password
            r = http_call("GET", API + "/auth/login?return_to=/equipas")
            check(r[0] == 302, "GET /auth/login -> 302 para o Zitadel")
            loc = r[1]["Location"]
            ck = cookies_of(r[1]).get("__Host-pitbox_login")
            check(ck is not None and "Lax" in ck[1].get("samesite", ""), "cookie de pré-login (SameSite=Lax)")
            q = urllib.parse.parse_qs(urllib.parse.urlparse(loc).query)
            check(q.get("code_challenge_method") == ["S256"] and "nonce" in q and "state" in q, "PKCE S256, nonce e state no pedido")
            state = q["state"][0]
            a = http_call("GET", loc)
            check(a[0] in (302, 303), "o Zitadel aceita o pedido de autorização")
            qa = urllib.parse.parse_qs(urllib.parse.urlparse(a[1]["Location"]).query)
            auth_request = (qa.get("authRequest") or qa.get("authRequestID") or [""])[0]
            check(auth_request != "", "auth request criado")
            sess = login.call("/v2/sessions", {"checks": {"user": {"loginName": current_login}, "password": {"password": current_password}}})
            cb = login.call(f"/v2/oidc/auth_requests/{auth_request}", {"session": {"sessionId": sess["sessionId"], "sessionToken": sess["sessionToken"]}})
            callback = cb["callbackUrl"]
            last["session_id"] = sess["sessionId"]
            check(callback.startswith(PUBLIC + "/auth/callback?"), "o Zitadel devolve o callback da API")
            secrets_seen.extend([sess["sessionToken"], urllib.parse.parse_qs(urllib.parse.urlparse(callback).query)["code"][0], state])
            return callback, ck[0], state

        print("== 3. Login (primeiro login: cria o utilizador PitBox)")
        callback, login_cookie, state = sign_in()
        # Sem cookie de pré-login e com state errado: 400 e nada de sessão.
        s1 = http_call("GET", callback)
        check(s1[0] == 400 and "__Host-pitbox_session" not in cookies_of(s1[1]), "callback sem cookie de pré-login -> 400")
        bad = http_call("GET", callback.replace(state, state[:-1] + ("A" if state[-1] != "A" else "B")), {"Cookie": cookie_header(**{"__Host-pitbox_login": login_cookie})})
        check(bad[0] == 400, "callback com state errado -> 400")
        # O cookie de pré-login é de uso único? Não: o servidor limpa-o, mas o código do IdP só vale uma vez.
        r = http_call("GET", callback, {"Cookie": cookie_header(**{"__Host-pitbox_login": login_cookie})})
        # o state errado não gastou o código; este é o callback legítimo
        check(r[0] == 200, "callback legítimo -> 200")
        body = r[2].decode()
        check('http-equiv="refresh"' in body and "/equipas" in body, "página 200 com meta refresh para o return_to")
        check(r[1]["Cache-Control"] == "no-store" and r[1]["Referrer-Policy"] == "no-referrer", "no-store e no-referrer")
        sc = cookies_of(r[1]).get("__Host-pitbox_session")
        check(sc is not None, "cookie de sessão definido")
        attrs = sc[1]
        check(attrs.get("secure") and attrs.get("httponly") and attrs.get("samesite", "").lower() == "strict" and attrs.get("path") == "/" and not attrs.get("domain"),
              "cookie __Host-: Secure, HttpOnly, SameSite=Strict, Path=/, sem Domain")
        session_cookie = sc[0]
        secrets_seen.append(session_cookie)

        def whoami(cookie, expect):
            return http_call("GET", API + "/auth/session", {"Cookie": cookie_header(**{"__Host-pitbox_session": cookie}), "Sec-Fetch-Site": "same-origin"}), expect

        (st, _, b), _ = whoami(session_cookie, 200)
        info = json.loads(b)
        check(st == 200 and info["authenticated"] is True and re.fullmatch(r"[0-9a-f-]{36}", info["user_id"]), "/auth/session -> utilizador autenticado")
        check("pwd" in info["amr"] and info["auth_time"], "amr e auth_time vêm da sessão (ID token)")
        user_id = info["user_id"]
        check(set(info) == {"authenticated", "user_id", "amr", "auth_time", "recovery_ready"}, "só identificadores na resposta (sem email, nome nem tokens)")
        check(info["recovery_ready"] is True, "recovery_ready: a conta com palavra-passe não precisa de aviso")
        x = http_call("GET", API + "/auth/session", {"Cookie": cookie_header(**{"__Host-pitbox_session": session_cookie}), "Origin": "https://evil.example"})
        check(x[0] == 403, "pedido de outra origem -> 403")

        print("== 4. Logout")
        bad_logout = http_call("POST", API + "/auth/logout", {"Cookie": cookie_header(**{"__Host-pitbox_session": session_cookie}), "Origin": "https://evil.example"}, b"")
        check(bad_logout[0] == 403, "logout com Origin errado -> 403 (a sessão continua)")
        check(whoami(session_cookie, 200)[0][0] == 200, "a sessão sobreviveu ao logout recusado")
        lo = http_call("POST", API + "/auth/logout", {"Cookie": cookie_header(**{"__Host-pitbox_session": session_cookie}), "Origin": PUBLIC}, b"")
        logout_url = meta_refresh_target(lo[2].decode()) if lo[0] == 200 else ""
        check(lo[0] == 200 and logout_url.startswith(ISSUER + "/oidc/v1/end_session?"), "logout -> 200 com meta refresh para o end_session do Zitadel")
        lq = urllib.parse.parse_qs(urllib.parse.urlparse(logout_url).query)
        check("id_token_hint" in lq and lq["post_logout_redirect_uri"] == [PUBLIC + "/auth/signed-out"], "id_token_hint e post_logout_redirect_uri")
        hint = lq["id_token_hint"][0].split(".")
        claims = json.loads(base64.urlsafe_b64decode(hint[1] + "=" * (-len(hint[1]) % 4)))
        check(not {"email", "email_verified", "name", "given_name", "family_name", "preferred_username"} & set(claims),
              "o id_token_hint (vai para o histórico do browser) não leva email nem nome")
        check("sub" in claims and "amr" in claims and "auth_time" in claims, "mas leva sub, amr e auth_time (a sessão precisa deles)")
        es = http_call("GET", logout_url)
        check(es[0] in (200, 302, 303), "o Zitadel aceita o end_session")
        check(whoami(session_cookie, 401)[0][0] == 401, "/auth/session -> 401 depois do logout")

        print("== 4b. Back-channel logout (#154, TM-AUTH-20): o Zitadel real termina a sessão do BFF")
        cb4, login_cookie4, _ = sign_in()
        r4 = http_call("GET", cb4, {"Cookie": cookie_header(**{"__Host-pitbox_login": login_cookie4})})
        bc = cookies_of(r4[1]).get("__Host-pitbox_session")
        check(r4[0] == 200 and bc is not None, "novo login cria a sessão do BFF")
        bc_cookie = bc[0]
        secrets_seen.append(bc_cookie)
        check(whoami(bc_cookie, 200)[0][0] == 200, "a sessão do BFF vale antes de o Zitadel a terminar")
        # Termina a sessão no Zitadel (sem passar pelo BFF): é ele que tem de avisar o pitbox-api por POST
        # a /auth/backchannel-logout, do contentor para o anfitrião (host.docker.internal).
        admin.call(f"/v2/sessions/{last['session_id']}", method="DELETE")
        ended = False
        for _ in range(30):  # o Zitadel envia o aviso de forma assíncrona (projeções)
            if whoami(bc_cookie, 401)[0][0] == 401:
                ended = True
                break
            time.sleep(1)
        check(ended, "o Zitadel avisou o BFF e a sessão acabou sem esperar pela renovação (back-channel logout)")

        print("== 4c. Recuperação de conta só com passkey (#158, ADR 0034)")
        recovery = provision.Zitadel(ZLOCAL, env["PITBOX_ZITADEL_RECOVERY_PAT"], ZHOST)
        # Os contadores por hora de uma execução anterior não podem contar nesta.
        vk = ["docker", "exec", "-e", f"REDISCLI_AUTH={env['VALKEY_PASSWORD']}", "pitbox-valkey-1", "valkey-cli", "--no-auth-warning"]
        for key in subprocess.run(vk + ["--scan", "--pattern", "pitbox:rl:rec:*"], capture_output=True, text=True, check=True).stdout.split():
            subprocess.run(vk + ["DEL", key], capture_output=True, check=True)
        pk_tag = secrets.token_hex(3)
        pk_email = f"pk-{pk_tag}@example.test"
        unknown_email = f"nobody-{pk_tag}@example.test"
        secrets_seen += [pk_email, unknown_email]
        pk_user = admin.call("/v2/users/human", {
            "username": pk_email, "profile": {"givenName": "Passkey", "familyName": "Only"},
            "email": {"email": pk_email, "isVerified": True},
        })["userId"]
        test_users.append(pk_user)
        passkey = softpasskey.SoftPasskey()
        try:
            domain = ZHOST.split(":")[0] if ZHOST else "localhost"
            reg = admin.call(f"/v2/users/{pk_user}/passkeys", {"authenticator": "PASSKEY_AUTHENTICATOR_UNSPECIFIED", "domain": domain})
            admin.call(f"/v2/users/{pk_user}/passkeys/{reg['passkeyId']}", {
                "publicKeyCredential": passkey.registration(reg["publicKeyCredentialCreationOptions"], ISSUER), "passkeyName": "smoke"})
        finally:
            passkey.close()
        methods = admin.call(f"/v2/users/{pk_user}/authentication_methods", method="GET").get("authMethodTypes", [])
        check(methods == ["AUTHENTICATION_METHOD_TYPE_PASSKEY"], "conta só com passkey criada")

        # O papel mínimo: a conta de serviço manda o link, mas não bloqueia, não repõe palavra-passe e não lê definições.
        denied = []
        for path, body, method in ((f"/v2/users/{pk_user}/lock", {}, "POST"), (f"/v2/users/{pk_user}/password_reset", {"returnCode": {}}, "POST"),
                                   ("/admin/v1/settings/oidc", None, "GET"), (f"/v2/users/{pk_user}", None, "DELETE")):
            try:
                recovery.call(path, body, method=method)
            except provision.ProvisionError as e:
                denied.append(" HTTP 403 " in str(e))
        check(denied == [True, True, True, True], "a conta de serviço da recuperação não bloqueia, não repõe palavra-passe, não lê definições nem apaga")

        def recover_request(addr):
            return http_call("POST", API + "/auth/recover", {"Content-Type": "application/json"}, json.dumps({"email": addr}).encode())

        clear_mailpit(env)
        answers = [recover_request(a) for a in (pk_email, unknown_email, email)]  # com passkey, desconhecido, só palavra-passe
        check(all(a[0] == 202 for a in answers), "/auth/recover -> 202 em todos os casos")
        check(len({a[2] for a in answers}) == 1 and len({(a[1]["Content-Type"], a[1]["Cache-Control"]) for a in answers}) == 1,
              "a resposta é igual exista a conta ou não (corpo e cabeçalhos)")
        reg_raw = latest_mailpit_raw(env)
        reg_msg = BytesParser(policy=policy.default).parsebytes(reg_raw)
        check(pk_email in str(reg_msg["To"]), "o email com o link de passkey chegou à conta só com passkey")
        reg_link = email_link(reg_raw, "passkey/set")
        secrets_seen += [reg_link, urllib.parse.parse_qs(urllib.parse.urlparse(reg_link).query).get("code", [""])[0]]
        time.sleep(3)  # dá tempo a um email indevido (conta desconhecida, conta com palavra-passe)
        st, _, body = mailpit_call(env, "GET", "/api/v1/messages?limit=50")
        check(st == 200 and json.loads(body).get("messages_count", json.loads(body).get("total", 0)) == 1,
              "só a conta com passkey recebeu email (a desconhecida e a de palavra-passe não)")
        # Anti-flood por conta: o 4.º pedido da mesma conta na hora não envia.
        for _ in range(3):
            recover_request(pk_email)
        time.sleep(3)
        st, _, body = mailpit_call(env, "GET", "/api/v1/messages?limit=50")
        check(json.loads(body).get("messages_count", json.loads(body).get("total", 0)) == 3, "no máximo 3 emails por conta e hora")
        bad = http_call("POST", API + "/auth/recover", {"Content-Type": "application/json"}, b'{"email":"nao-e-email"}')
        check(bad[0] == 400, "email inválido -> 400")

        print("== 5. Segundo login do mesmo utilizador")
        callback2, lc2, _ = sign_in()
        r2 = http_call("GET", callback2, {"Cookie": cookie_header(**{"__Host-pitbox_login": lc2})})
        check(r2[0] == 200, "segundo login -> 200")
        sc2 = cookies_of(r2[1])["__Host-pitbox_session"][0]
        secrets_seen.append(sc2)
        check(sc2 != session_cookie, "cookie de sessão novo")
        info2 = json.loads(whoami(sc2, 200)[0][2])
        check(info2["user_id"] == user_id, "o mesmo utilizador PitBox (não duplica)")

        # Um login que o browser começou (state certo) mas com um código que o Zitadel nunca emitiu.
        r_start = http_call("GET", API + "/auth/login")
        st_ok = urllib.parse.parse_qs(urllib.parse.urlparse(r_start[1]["Location"]).query)["state"][0]
        bogus = http_call("GET", API + "/auth/callback?code=not-a-real-code&state=" + urllib.parse.quote(st_ok),
                          {"Cookie": cookie_header(**{"__Host-pitbox_login": cookies_of(r_start[1])["__Host-pitbox_login"][0]})})
        check(bogus[0] == 400, "state certo com um código que o Zitadel não emitiu -> 400")

        print("== 5b. Login móvel (ADR 0032): Bearer opaco, prova de posse, reutilização e logout")
        mobile_redirect = "app.pitbox.mobile:/auth/callback"
        vcli = lambda *a: subprocess.run(["docker", "exec", "-e", f"REDISCLI_AUTH={env['VALKEY_PASSWORD']}", "pitbox-valkey-1", "valkey-cli", "--no-auth-warning", *a],
                                         capture_output=True, text=True, check=True).stdout.strip()

        def mobile_login(key):
            verifier, challenge = pkce_pair()
            st, start = post_json(API + "/auth/mobile/start", {"session_pub": key.spki, "code_challenge": challenge})
            check(st == 200 and set(start) == {"authorization_url", "state"}, "POST /auth/mobile/start -> 200 com o URL de autorização")
            qs = urllib.parse.parse_qs(urllib.parse.urlparse(start["authorization_url"]).query)
            check(qs["client_id"] == [env["PITBOX_OIDC_MOBILE_CLIENT_ID"]] and qs["redirect_uri"] == [mobile_redirect]
                  and qs["code_challenge_method"] == ["S256"] and qs.get("prompt") == ["login"] and "client_secret" not in qs,
                  "cliente nativo público, redirect próprio, PKCE S256 e prompt=login")
            a = http_call("GET", start["authorization_url"])
            check(a[0] in (302, 303), "o Zitadel aceita o pedido do cliente nativo")
            qa = urllib.parse.parse_qs(urllib.parse.urlparse(a[1]["Location"]).query)
            auth_request = (qa.get("authRequest") or qa.get("authRequestID") or [""])[0]
            check(auth_request != "", "auth request do cliente nativo criado")
            sess = login.call("/v2/sessions", {"checks": {"user": {"loginName": login_name}, "password": {"password": password}}})
            cb = login.call(f"/v2/oidc/auth_requests/{auth_request}", {"session": {"sessionId": sess["sessionId"], "sessionToken": sess["sessionToken"]}})
            check(cb["callbackUrl"].startswith(mobile_redirect + "?"), "o Zitadel devolve o redirect do esquema próprio")
            cq = urllib.parse.parse_qs(urllib.parse.urlparse(cb["callbackUrl"]).query)
            code, state_m = cq["code"][0], cq["state"][0]
            check(state_m == start["state"], "o state do redirect é o do /start")
            secrets_seen.extend([sess["sessionToken"], code, state_m, verifier])
            return verifier, state_m, code

        key = DeviceKey(work)
        verifier, state_m, code = mobile_login(key)
        body_ok = {"state": state_m, "code": code, "code_verifier": verifier, "proof": key.proof("pitbox-mobile-v1/token", state_m, code)}
        other = DeviceKey(work)
        st, _ = post_json(API + "/auth/mobile/token", dict(body_ok, proof=other.proof("pitbox-mobile-v1/token", state_m, code)))
        check(st == 400, "prova assinada por outra chave -> 400 (quem apanha o código não tem a chave)")
        st, _ = post_json(API + "/auth/mobile/token", body_ok)
        check(st == 400, "o state foi gasto pela tentativa recusada (uso único)")
        verifier, state_m, code = mobile_login(key)
        st, tok = post_json(API + "/auth/mobile/token", {"state": state_m, "code": code, "code_verifier": verifier, "proof": key.proof("pitbox-mobile-v1/token", state_m, code)})
        check(st == 200 and set(tok) == {"session", "access_token", "expires_in"} and tok["access_token"].startswith("pbm1_") and tok["expires_in"] == 600,
              "troca do código -> sessão e Bearer opaco de 10 min (sem refresh token nem ID token)")
        secrets_seen.append(tok["access_token"])
        bearer = {"Authorization": "Bearer " + tok["access_token"]}
        who = http_call("GET", API + "/auth/session", bearer)
        check(who[0] == 200 and json.loads(who[2])["user_id"] == user_id, "/auth/session com o Bearer -> o mesmo utilizador PitBox")
        mix = http_call("GET", API + "/auth/session", dict(bearer, Cookie=cookie_header(**{"__Host-pitbox_session": sc2})))
        check(mix[0] == 401, "Bearer e cookie no mesmo pedido -> 401")
        check(http_call("GET", API + "/auth/session", {"Authorization": "Bearer " + "a.b.c"})[0] == 401, "um JWT como Bearer -> 401")
        pre = http_call("OPTIONS", API + "/auth/session", {"Origin": "https://localhost", "Access-Control-Request-Method": "GET", "Access-Control-Request-Headers": "authorization"})
        check(pre[0] == 204 and pre[1].get("Access-Control-Allow-Origin") == "https://localhost" and "Access-Control-Allow-Credentials" not in pre[1], "CORS: preflight do WebView -> 204, sem credenciais")
        pre = http_call("OPTIONS", API + "/auth/session", {"Origin": "https://evil.example", "Access-Control-Request-Method": "GET"})
        check(pre[0] == 403 and "Access-Control-Allow-Origin" not in pre[1], "CORS: preflight de outra origem -> 403")

        sref = tok["session"]
        rbody = lambda n: {"session": sref, "counter": n, "proof": key.proof("pitbox-mobile-v1/refresh", sref, str(n))}
        st, bad = post_json(API + "/auth/mobile/refresh", {"session": sref, "counter": 1, "proof": other.proof("pitbox-mobile-v1/refresh", sref, "1")})
        check(st == 401, "renovação com prova de outra chave -> 401 (e não gasta o contador)")
        st, r1 = post_json(API + "/auth/mobile/refresh", rbody(1))
        check(st == 200 and r1["access_token"] != tok["access_token"], "renovação com contador 1 -> novo Bearer")
        secrets_seen.append(r1["access_token"])
        check(http_call("GET", API + "/auth/session", bearer)[0] == 401, "o Bearer anterior deixou de valer logo")
        newer = {"Authorization": "Bearer " + r1["access_token"]}
        check(http_call("GET", API + "/auth/session", newer)[0] == 200, "o Bearer novo vale")
        st, _ = post_json(API + "/auth/mobile/refresh", rbody(1))
        check(st == 401, "renovação repetida (contador 1) -> 401")
        check(vcli("EXISTS", "pitbox:sess:" + sref) == "0", "a reutilização apagou a sessão do Valkey")
        check(http_call("GET", API + "/auth/session", newer)[0] == 401, "e o Bearer legítimo deixou de valer (alarme de reutilização)")

        verifier, state_m, code = mobile_login(key)
        st, tok2 = post_json(API + "/auth/mobile/token", {"state": state_m, "code": code, "code_verifier": verifier, "proof": key.proof("pitbox-mobile-v1/token", state_m, code)})
        check(st == 200, "novo login móvel depois do alarme")
        secrets_seen.append(tok2["access_token"])
        b2 = {"Authorization": "Bearer " + tok2["access_token"]}
        st, _, _ = http_call("POST", API + "/auth/mobile/logout", dict(b2, **{"Content-Type": "application/json"}), b"{}")
        check(st == 204, "logout móvel -> 204")
        check(http_call("GET", API + "/auth/session", b2)[0] == 401, "o Bearer não vale depois do logout")
        check(vcli("EXISTS", "pitbox:sess:" + tok2["session"]) == "0", "a sessão foi apagada")
        check(not [k for k in vcli("KEYS", "pitbox:mob:*").splitlines() if tok["access_token"][5:] in k or tok2["access_token"][5:] in k],
              "o valor do Bearer não está em nenhuma chave do Valkey")

        print("== 6. Refresh real (access token de 5 s) e utilizador desativado")
        short = dict(provision.OIDC_SETTINGS, accessTokenLifetime=5)
        admin.call("/admin/v1/settings/oidc", {k: f"{v}s" for k, v in short.items()}, method="PUT")
        shortened = True
        callback3, lc3, _ = sign_in()
        r3 = http_call("GET", callback3, {"Cookie": cookie_header(**{"__Host-pitbox_login": lc3})})
        check(r3[0] == 200, "login com access token de 5 s")
        sc3 = cookies_of(r3[1])["__Host-pitbox_session"][0]
        secrets_seen.append(sc3)
        ref3 = __import__("hashlib").sha256(sc3.encode()).hexdigest()

        def generation():
            return subprocess.run(["docker", "exec", "-e", f"REDISCLI_AUTH={env['VALKEY_PASSWORD']}", "pitbox-valkey-1", "valkey-cli", "--no-auth-warning",
                                   "HGET", "pitbox:sess:" + ref3, "g"], capture_output=True, text=True, check=True).stdout.strip()
        check(generation() == "1", "sessão nova na geração 1")
        time.sleep(7)  # o access token expirou
        st3 = whoami(sc3, 200)[0]
        check(st3[0] == 200 and json.loads(st3[2])["user_id"] == user_id, "a API renovou o token e a sessão continua")
        check(generation() == "2", "refresh a sério: a sessão passou à geração 2 (refresh token rodado)")
        time.sleep(7)
        check(whoami(sc3, 200)[0][0] == 200 and generation() == "3", "segundo refresh com o token rodado: continua")
        admin.call(f"/v2/users/{zitadel_user}/deactivate", {})
        time.sleep(7)
        gone = whoami(sc3, 401)[0]
        check(gone[0] == 401, "utilizador desativado: o refresh falha e a sessão acaba (401)")
        check(subprocess.run(["docker", "exec", "-e", f"REDISCLI_AUTH={env['VALKEY_PASSWORD']}", "pitbox-valkey-1", "valkey-cli", "--no-auth-warning", "EXISTS", "pitbox:sess:" + ref3],
                             capture_output=True, text=True, check=True).stdout.strip() == "0", "a sessão foi apagada do Valkey")
        admin.call("/admin/v1/settings/oidc", {k: f"{v}s" for k, v in provision.OIDC_SETTINGS.items()}, method="PUT")
        shortened = False

        print("== 7. Auditoria, logs e Valkey")
        sql = lambda q: psql(env, q)
        rows = sql(f"SELECT action||':'||coalesce(after->>'reason','') FROM audit.audit_log WHERE actor_id='{user_id}' OR (actor_id='anonymous' AND occurred_at > now() - interval '5 minutes') ORDER BY id")
        got = rows.splitlines()
        check(got.count("auth.login:") == 4 and got.count("auth.logout:") == 2, "auditoria: 4 logins e 2 logouts do utilizador (web e móvel)")
        # O back-channel é auditado com o `sub` do IdP como autor (não há sessão do BFF nesse pedido).
        check(int(sql("SELECT count(*) FROM audit.audit_log WHERE action='auth.backchannel_logout' AND occurred_at > now() - interval '10 minutes'")) >= 1,
              "auditoria: o back-channel logout ficou registado")
        rec_rows = sql("SELECT actor_id||':'||coalesce(after->>'reason','') FROM audit.audit_log WHERE action='auth.recovery_requested' AND occurred_at > now() - interval '10 minutes' ORDER BY id").splitlines()
        check(sum(r.endswith(":sent") for r in rec_rows) == 3 and any(r.endswith(":no_account") for r in rec_rows)
              and any(r.endswith(":no_passkey") for r in rec_rows) and any(r.endswith(":throttled") for r in rec_rows),
              "auditoria: pedidos de recuperação com o motivo (enviado, sem conta, sem passkey, limitado)")
        check(not any("@" in r or "example.test" in r for r in rec_rows), "auditoria: nenhum email, só pseudónimos")
        check(got.count("auth.login:mobile") == 2 and "auth.mobile_reuse:refresh_reuse" in got, "auditoria: 2 logins móveis e o alarme de reutilização")
        check("auth.session_revoked:user_not_active" in got, "auditoria: sessão revogada porque o utilizador foi desativado")
        check(not any(g.startswith("auth.login_failed:") and g.split(":", 1)[1] in ("no_login_state", "state_mismatch") for g in got),
              "callbacks sem cookie ou com state errado NÃO foram para a auditoria (só log e contador)")
        check("auth.login_failed:code_exchange" in got, "auditoria: falha depois de o state bater certo (código inválido), com o motivo")
        check(sql(f"SELECT count(*) FROM identity.user_account WHERE id='{user_id}'") == "1", "uma só linha identity.user_account")
        log_text = Path(logpath).read_text()
        leaked = [i for i, sct in enumerate(secrets_seen) if sct and sct in log_text]
        check(not leaked, "nenhum segredo (cookie, código, state, palavra-passe, email) nos logs da API")
        vkeys = subprocess.run(["docker", "exec", "-e", f"REDISCLI_AUTH={env['VALKEY_PASSWORD']}", "pitbox-valkey-1", "valkey-cli", "--no-auth-warning", "KEYS", "pitbox:*"],
                               capture_output=True, text=True, check=True).stdout
        check(session_cookie not in vkeys and sc2 not in vkeys, "o valor do cookie não está em nenhuma chave do Valkey (só o hash)")
        jwks = json.loads(http_call("GET", ISSUER + "/oauth/v2/keys")[2])
        check(any(k.get("alg") == "ES256" for k in jwks["keys"]), "o Zitadel assina com ES256 (o login só aceita ES256)")
        print("== 8. Registo, verificação de email e página de confirmação")
        check(bool(env.get("MAILPIT_UI_USERNAME") and env.get("MAILPIT_UI_PASSWORD")),
              "Mailpit UI/API autenticado")
        clear_mailpit(env)
        new_tag = secrets.token_hex(4)
        new_username = f"pitbox-verify-{new_tag}"
        new_email = f"verify-{new_tag}@example.eu"
        new_password = "Aa1!" + secrets.token_urlsafe(18)
        secrets_seen += [new_password, new_email]
        created_new = admin.call("/v2/users/human", {
            "username": new_username,
            "profile": {"givenName": "Verify", "familyName": "Test", "preferredLanguage": "pt"},
            "email": {"email": new_email, "sendCode": {}},
            "password": {"password": new_password, "changeRequired": False},
        })
        new_user = created_new["userId"]
        test_users.append(new_user)
        new_login_name = admin.call(f"/v2/users/{new_user}", method="GET")["user"]["preferredLoginName"]
        check(True, "utilizador novo criado sem email verificado")

        callback_nv, cookie_nv, _ = sign_in(new_login_name, new_password)
        r_nv = http_call("GET", callback_nv, {"Cookie": cookie_header(**{"__Host-pitbox_login": cookie_nv})})
        check(r_nv[0] == 303 and "__Host-pitbox_session" not in cookies_of(r_nv[1]),
              "email por verificar -> 303 para /confirm-email, sem sessão")
        check("/confirm-email?return_to=%2Fequipas" in r_nv[1].get("Location", ""),
              "email por verificar -> /confirm-email com return_to seguro")

        raw_email = latest_mailpit_raw(env)
        link = verification_link(raw_email)
        secrets_seen.append(link)
        query = urllib.parse.parse_qs(urllib.parse.urlparse(link).query)
        code = query.get("code", [""])[0]
        link_user = (query.get("userId") or query.get("userID") or [""])[0]
        check(bool(code) and link_user == new_user, "link de verificação com código e utilizador")
        secrets_seen.append(code)
        page = http_call("GET", link)
        check(page[0] in (200, 302, 303), "página de verificação do Zitadel responde")
        admin.call(f"/v2/users/{new_user}/email/verify", {"userId": new_user, "verificationCode": code})
        check(True, "email verificado pelo código do Mailpit")

        callback_ok, cookie_ok, _ = sign_in(new_login_name, new_password)
        r_ok = http_call("GET", callback_ok, {"Cookie": cookie_header(**{"__Host-pitbox_login": cookie_ok})})
        check(r_ok[0] == 200 and "__Host-pitbox_session" in cookies_of(r_ok[1]),
              "depois de verificar, login cria sessão")
        session_ok = cookies_of(r_ok[1])["__Host-pitbox_session"][0]
        secrets_seen.append(session_ok)
        info_ok = json.loads(http_call("GET", API + "/auth/session", {
            "Cookie": cookie_header(**{"__Host-pitbox_session": session_ok}),
            "Sec-Fetch-Site": "same-origin",
        })[2])
        check(info_ok.get("authenticated") is True, "sessão da conta verificada válida")

        print("== 9. Recuperação de palavra-passe")
        clear_mailpit(env)
        admin.call(f"/v2/users/{new_user}/password_reset", {
            "sendLink": {"notificationType": "NOTIFICATION_TYPE_Email"},
        })
        reset_raw = latest_mailpit_raw(env)
        reset_link = email_link(reset_raw, "password/set")
        reset_query = urllib.parse.parse_qs(urllib.parse.urlparse(reset_link).query)
        reset_code = reset_query.get("code", [""])[0]
        reset_user = (reset_query.get("userId") or reset_query.get("userID") or [""])[0]
        secrets_seen += [reset_link, reset_code]
        check(bool(reset_code) and reset_user == new_user, "email de recuperação com código e utilizador")
        reset_password = "Aa1!" + secrets.token_urlsafe(18)
        secrets_seen.append(reset_password)
        admin.call(f"/v2/users/{new_user}/password", {
            "userId": new_user,
            "newPassword": {"password": reset_password},
            "verificationCode": reset_code,
        })
        callback_reset, cookie_reset, _ = sign_in(new_login_name, reset_password)
        reset_response = http_call("GET", callback_reset, {
            "Cookie": cookie_header(**{"__Host-pitbox_login": cookie_reset}),
        })
        check(reset_response[0] == 200 and "__Host-pitbox_session" in cookies_of(reset_response[1]),
              "palavra-passe recuperada e login com a nova")

        print("== 10. Lockout, política de password e rate limit")
        wrong = "wrong-" + secrets.token_urlsafe(12)
        for _ in range(10):
            try:
                login.call("/v2/sessions", {"checks": {"user": {"loginName": new_login_name}, "password": {"password": wrong}}})
                fail("password errada aceite")
            except provision.ProvisionError:
                pass
        login.call("/v2/sessions", {"checks": {"user": {"loginName": new_login_name}, "password": {"password": reset_password}}})
        check(True, "10 falhas não bloqueiam a conta")

        for candidate, should_pass in (("x" * 11, False), ("abcdefghijkl", True)):
            tag2 = secrets.token_hex(3)
            try:
                made = admin.call("/v2/users/human", {
                    "username": f"pitbox-pw-{tag2}",
                    "profile": {"givenName": "Pw", "familyName": "Test"},
                    "email": {"email": f"pw-{tag2}@example.eu", "isVerified": True},
                    "password": {"password": candidate, "changeRequired": False},
                })
                test_users.append(made["userId"])
                passed = True
            except provision.ProvisionError:
                passed = False
            check(passed is should_pass, f"password de {len(candidate)} caracteres {'aceite' if should_pass else 'recusada'}")

        codes = []
        for _ in range(11):
            status, _, _ = http_call(
                "POST", ISSUER + "/ui/v2/login",
                {"Content-Type": "application/x-www-form-urlencoded"}, b"",
            )
            codes.append(status)
        check(codes[-1] == 429 and any(code < 400 for code in codes[:-1]),
              "11 POST ao login v2 do mesmo IP -> 429")

        log_text = Path(logpath).read_text()
        check(not any(secret and secret in log_text for secret in secrets_seen),
              "nenhum segredo novo (password, email, código, link, cookie) nos logs da API")
        access_logs = subprocess.run(
            ["docker", "logs", "pitbox-zitadel-proxy-1", "pitbox-zitadel-login-1"],
            capture_output=True, text=True, check=False,
        )
        access_text = access_logs.stdout + access_logs.stderr
        check(code not in access_text and link not in access_text,
              "código/link de verificação não está nos logs do Traefik/Zitadel Login")

        print("auth-smoke: OK")
        return 0
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=10)
        except subprocess.TimeoutExpired:
            proc.kill()
        logf.close()
        if admin_pat and shortened:
            try:  # nunca deixar o Zitadel com o access token de 5 s
                provision.Zitadel(ZLOCAL, admin_pat, ZHOST).call("/admin/v1/settings/oidc", {k: f"{v}s" for k, v in provision.OIDC_SETTINGS.items()}, method="PUT")
            except Exception:  # noqa: BLE001
                print("auth-smoke: AVISO: repõe os tempos dos tokens com `make zitadel-provision`", file=sys.stderr)
        if admin_pat and 'test_users' in locals():
            admin = provision.Zitadel(ZLOCAL, admin_pat, ZHOST)
            for uid in test_users:
                try:
                    admin.call(f"/v2/users/{uid}", method="DELETE")
                except Exception:  # noqa: BLE001 — limpeza; não esconde o resultado do teste
                    print("auth-smoke: aviso: não consegui apagar um utilizador de teste", file=sys.stderr)
        # Linhas criadas pelo primeiro login: pelo idp_subject (= id no Zitadel) dos utilizadores
        # que ESTE script criou, também em falha. Nunca por email nem por padrão.
        failing = sys.exc_info()[0] is not None
        try:
            removed = cleanup_user_accounts(lambda q: psql(env, q), locals().get("test_users", []))
            accounts_after = int(psql(env, "SELECT count(*) FROM identity.user_account"))
            print(f"auth-smoke: limpeza: {removed} linha(s) de user_account apagadas; contagem {accounts_before} -> {accounts_after}")
            if accounts_after != accounts_before:
                msg = "a contagem de identity.user_account mudou (uso em paralelo ou linha por limpar?)"
                if failing:
                    print(f"auth-smoke: AVISO: {msg}", file=sys.stderr)
                else:
                    fail(msg)
        except (subprocess.CalledProcessError, ValueError, OSError):  # sem detalhes: o erro do psql pode ter dados
            print("auth-smoke: AVISO: não consegui limpar identity.user_account", file=sys.stderr)


if __name__ == "__main__":
    sys.exit(main())
