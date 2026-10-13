"""Prova o login do pitbox-api ponta a ponta contra o Zitadel local, sem browser (F1-03).

Faz o que o browser faria, passo a passo, e confirma cada passo:
  1. arranca o binário com a configuração local (precisa de `make up` e `make zitadel-provision`);
  2. cria um utilizador de teste no Zitadel (email verificado, palavra-passe aleatória);
  3. GET /auth/login -> redireciona para o Zitadel; segue o authorize até ao auth request;
  4. conclui o login do lado do Zitadel pela API de sessões (padrão das UIs de login próprias:
     criar sessão com utilizador + palavra-passe, e finalizar o auth request);
  5. entrega o callback à API: cria o utilizador no primeiro login e a sessão (cookie __Host-);
  6. /auth/session responde com o utilizador; um callback sem cookie e um com state errado dão 400;
  7. logout (POST com Origin) -> 303 para o end_session do Zitadel; /auth/session passa a 401;
  8. segundo login do mesmo utilizador -> o MESMO user_id (não duplica);
  9. refresh a sério: encurta o access token para 5 s no Zitadel, espera que expire e confirma que
     a API renova (a geração da sessão no Valkey sobe) sem pedir novo login; depois desativa o
     utilizador e confirma que o refresh seguinte falha e a sessão acaba (UNAUTHENTICATED);
 10. auditoria na base (login, logout, falhas, sessão revogada), nada de segredos nos logs, nada
     do valor do cookie no Valkey; repõe os tempos dos tokens e apaga o utilizador de teste.

Usa o PAT do login-client (só sessões) para o login e o do provisionamento só para criar e apagar
o utilizador de teste. Nenhum PAT nem palavra-passe é impresso. Só biblioteca padrão.
"""
import http.client
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
API = "http://localhost:8081"  # tem de coincidir com o redirect URI registado (PITBOX_PUBLIC_URL)

spec = importlib.util.spec_from_file_location("provision", ROOT / "scripts" / "zitadel-provision.py")
provision = importlib.util.module_from_spec(spec)
spec.loader.exec_module(provision)


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


def http_call(method, url, headers=None, body=None):
    """Devolve (status, headers, corpo); nunca segue redirects."""
    req = urllib.request.Request(url, data=body, method=method, headers=headers or {})
    try:
        with OPENER.open(req, timeout=15) as r:
            return r.status, r.headers, r.read()
    except urllib.error.HTTPError as e:
        return e.code, e.headers, e.read()


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


def main() -> int:
    if not ENV_FILE.exists():
        fail("falta deploy/compose/.env (make init)")
    env = provision.parse_env(ENV_FILE.read_text())
    for k in ("PITBOX_OIDC_CLIENT_ID", "PITBOX_OIDC_CLIENT_SECRET", "PITBOX_AUTH_KEYS", "VALKEY_API_PASSWORD", "PITBOX_API_PASSWORD"):
        if not env.get(k):
            fail(f"falta {k} no .env (make init; make zitadel-provision)")
    zport = env.get("ZITADEL_PORT", "8080")
    zitadel = f"http://localhost:{zport}"

    try:
        socket.create_connection(("127.0.0.1", 8081), timeout=0.5).close()
        fail("a porta 8081 já está ocupada (pára o make run-api)")
    except OSError:
        pass

    work = tempfile.mkdtemp(prefix="pitbox-auth-smoke-")
    binary, logpath = os.path.join(work, "api"), os.path.join(work, "log")
    print("== 1. Compilar e arrancar o pitbox-api")
    subprocess.run(["go", "build", "-o", binary, "./cmd/api"], cwd=ROOT / "backend", check=True)
    child_env = {
        "PATH": os.environ["PATH"], "PITBOX_ENV": "local", "PITBOX_HTTP_ADDR": "127.0.0.1:8081", "PITBOX_PUBLIC_URL": API,
        "PITBOX_OIDC_ISSUER": zitadel, "PITBOX_OIDC_CLIENT_ID": env["PITBOX_OIDC_CLIENT_ID"],
        "PITBOX_OIDC_CLIENT_SECRET": env["PITBOX_OIDC_CLIENT_SECRET"], "PITBOX_AUTH_KEYS": env["PITBOX_AUTH_KEYS"],
        "PITBOX_VALKEY_ADDR": f"127.0.0.1:{env.get('VALKEY_PORT', '6379')}", "PITBOX_VALKEY_PASSWORD": env["VALKEY_API_PASSWORD"],
        "PITBOX_DATABASE_URL": f"postgres://pitbox_api:{env['PITBOX_API_PASSWORD']}@127.0.0.1:{env.get('POSTGRES_PORT', '5432')}/pitbox?sslmode=disable",
    }
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
        admin = provision.Zitadel(zitadel, admin_pat)
        login = provision.Zitadel(zitadel, login_pat)

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
        login_name = admin.call(f"/v2/users/{zitadel_user}", method="GET")["user"]["preferredLoginName"]
        secrets_seen += [email]
        check(True, "utilizador criado (email verificado)")

        def sign_in():
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
            sess = login.call("/v2/sessions", {"checks": {"user": {"loginName": login_name}, "password": {"password": password}}})
            cb = login.call(f"/v2/oidc/auth_requests/{auth_request}", {"session": {"sessionId": sess["sessionId"], "sessionToken": sess["sessionToken"]}})
            callback = cb["callbackUrl"]
            check(callback.startswith(API + "/auth/callback?"), "o Zitadel devolve o callback da API")
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
        check(set(info) == {"authenticated", "user_id", "amr", "auth_time"}, "só identificadores na resposta (sem email, nome nem tokens)")
        x = http_call("GET", API + "/auth/session", {"Cookie": cookie_header(**{"__Host-pitbox_session": session_cookie}), "Origin": "https://evil.example"})
        check(x[0] == 403, "pedido de outra origem -> 403")

        print("== 4. Logout")
        bad_logout = http_call("POST", API + "/auth/logout", {"Cookie": cookie_header(**{"__Host-pitbox_session": session_cookie}), "Origin": "https://evil.example"}, b"")
        check(bad_logout[0] == 403, "logout com Origin errado -> 403 (a sessão continua)")
        check(whoami(session_cookie, 200)[0][0] == 200, "a sessão sobreviveu ao logout recusado")
        lo = http_call("POST", API + "/auth/logout", {"Cookie": cookie_header(**{"__Host-pitbox_session": session_cookie}), "Origin": API}, b"")
        check(lo[0] == 303 and lo[1]["Location"].startswith(zitadel + "/oidc/v1/end_session?"), "logout -> 303 para o end_session do Zitadel")
        lq = urllib.parse.parse_qs(urllib.parse.urlparse(lo[1]["Location"]).query)
        check("id_token_hint" in lq and lq["post_logout_redirect_uri"] == [API + "/auth/signed-out"], "id_token_hint e post_logout_redirect_uri")
        es = http_call("GET", lo[1]["Location"])
        check(es[0] in (200, 302, 303), "o Zitadel aceita o end_session")
        check(whoami(session_cookie, 401)[0][0] == 401, "/auth/session -> 401 depois do logout")

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
        sql = lambda q: subprocess.run(
            ["docker", "exec", "-e", f"PGPASSWORD={env['POSTGRES_SUPERUSER_PASSWORD']}", "pitbox-postgres-1", "psql", "-X", "-At", "-h", "127.0.0.1", "-U", "postgres", "-d", "pitbox", "-c", q],
            capture_output=True, text=True, check=True).stdout.strip()
        rows = sql(f"SELECT action||':'||coalesce(after->>'reason','') FROM audit.audit_log WHERE actor_id='{user_id}' OR (actor_id='anonymous' AND occurred_at > now() - interval '5 minutes') ORDER BY id")
        got = rows.splitlines()
        check(got.count("auth.login:") == 3 and got.count("auth.logout:") == 1, "auditoria: 3 logins e 1 logout do utilizador")
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
        jwks = json.loads(http_call("GET", zitadel + "/oauth/v2/keys")[2])
        check(any(k.get("alg") == "ES256" for k in jwks["keys"]), "o Zitadel assina com ES256 (o login só aceita ES256)")
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
                provision.Zitadel(zitadel, admin_pat).call("/admin/v1/settings/oidc", {k: f"{v}s" for k, v in provision.OIDC_SETTINGS.items()}, method="PUT")
            except Exception:  # noqa: BLE001
                print("auth-smoke: AVISO: repõe os tempos dos tokens com `make zitadel-provision`", file=sys.stderr)
        if admin_pat and 'zitadel_user' in locals():
            try:
                provision.Zitadel(zitadel, admin_pat).call(f"/v2/users/{zitadel_user}", method="DELETE")
            except Exception:  # noqa: BLE001 — limpeza; não esconde o resultado do teste
                print("auth-smoke: aviso: não consegui apagar o utilizador de teste", file=sys.stderr)


if __name__ == "__main__":
    sys.exit(main())
