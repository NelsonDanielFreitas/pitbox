"""Testes do zitadel-provision.py contra um Zitadel falso (sem Docker).

Correr: python3 -I -m unittest discover -s scripts -p 'test_*.py'
O servidor falso guarda estado, por isso a segunda execução prova a idempotência.
"""
import importlib.util
import io
import json
import os
import stat
import tempfile
import threading
import unittest
from contextlib import redirect_stdout
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path

SPEC = importlib.util.spec_from_file_location("provision", Path(__file__).with_name("zitadel-provision.py"))
prov = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(prov)

PAT = "pat-segredo-de-teste"
SECRET = "segredo-do-cliente-de-teste"


class FakeState:
    def __init__(self):
        self.calls = []  # (método, caminho) das chamadas que mudam estado
        self.webkeys = [{"id": "1", "state": "STATE_ACTIVE", "rsa": {}}]
        self.oidc = {"accessTokenLifetime": "43200s", "idTokenLifetime": "43200s",
                     "refreshTokenIdleExpiration": "2592000s", "refreshTokenExpiration": "7776000s"}
        self.factors = ["SECOND_FACTOR_TYPE_OTP", "SECOND_FACTOR_TYPE_OTP_SMS", "SECOND_FACTOR_TYPE_OTP_EMAIL"]
        self.login_policy = {
            "allowUsernamePassword": True,
            "allowRegister": False,
            "allowExternalIdp": True,
            "forceMfa": False,
            "passwordlessType": "PASSWORDLESS_TYPE_NOT_ALLOWED",
            "hidePasswordReset": False,
            "ignoreUnknownUsernames": False,
            "defaultRedirectUri": "",
            "passwordCheckLifetime": "86400s",
            "externalLoginCheckLifetime": "86400s",
            "mfaInitSkipLifetime": "2592000s",
            "secondFactorCheckLifetime": "64800s",
            "multiFactorCheckLifetime": "43200s",
            "allowDomainDiscovery": True,
            "disableLoginWithEmail": False,
            "disableLoginWithPhone": False,
            "forceMfaLocalOnly": False,
        }
        self.complexity = {"minLength": 8, "hasUppercase": True, "hasLowercase": True,
                           "hasNumber": True, "hasSymbol": True}
        self.lockout = {"maxPasswordAttempts": 10, "maxOtpAttempts": 10}
        self.generators = {
            "SECRET_GENERATOR_TYPE_VERIFY_EMAIL_CODE": {
                "generatorType": "SECRET_GENERATOR_TYPE_VERIFY_EMAIL_CODE", "length": 6,
                "expiry": "3600s", "includeLowerLetters": False, "includeUpperLetters": True,
                "includeDigits": True, "includeSymbols": False,
            },
            "SECRET_GENERATOR_TYPE_PASSWORD_RESET_CODE": {
                "generatorType": "SECRET_GENERATOR_TYPE_PASSWORD_RESET_CODE", "length": 6,
                "expiry": "3600s", "includeLowerLetters": False, "includeUpperLetters": True,
                "includeDigits": True, "includeSymbols": False,
            },
        }
        self.language = "en"
        self.texts = {}
        self.smtp = []
        self.smtp_password = None
        self.projects = []
        self.apps = []


def make_handler(st: FakeState):
    class H(BaseHTTPRequestHandler):
        def log_message(self, *a):
            pass

        def _send(self, obj, code=200):
            raw = json.dumps(obj).encode()
            self.send_response(code)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(raw)))
            self.end_headers()
            self.wfile.write(raw)

        def _body(self):
            n = int(self.headers.get("Content-Length") or 0)
            return json.loads(self.rfile.read(n)) if n else {}

        def handle_any(self, method):
            path = self.path
            if path == "/.well-known/openid-configuration":
                return self._send({})
            if self.headers.get("Authorization") != f"Bearer {PAT}":
                return self._send({"code": 16}, 401)
            body = self._body() if method in ("POST", "PUT") else {}
            if path.endswith("/ListWebKeys"):
                return self._send({"webKeys": st.webkeys})
            if path.endswith("/CreateWebKey"):
                st.calls.append((method, path))
                st.webkeys.append({"id": "2", "state": "STATE_INITIAL", "ecdsa": {"curve": "ECDSA_CURVE_P256"}})
                return self._send({"id": "2"})
            if path.endswith("/ActivateWebKey"):
                st.calls.append((method, path))
                for k in st.webkeys:
                    k["state"] = "STATE_ACTIVE" if k["id"] == body["id"] else "STATE_INACTIVE"
                return self._send({})
            if path == "/admin/v1/settings/oidc":
                if method == "GET":
                    return self._send({"settings": st.oidc})
                st.calls.append((method, path))
                st.oidc = body
                return self._send({})
            if path == "/admin/v1/policies/login/second_factors/_search":
                return self._send({"result": st.factors})
            if path.startswith("/admin/v1/policies/login/second_factors/") and method == "DELETE":
                st.calls.append((method, path))
                st.factors.remove(path.rsplit("/", 1)[1])
                return self._send({})
            if path == "/admin/v1/policies/login":
                if method == "GET":
                    return self._send({"policy": st.login_policy})
                st.calls.append((method, path))
                st.login_policy = body
                return self._send({})
            if path == "/admin/v1/policies/password/complexity":
                if method == "GET":
                    return self._send({"policy": st.complexity})
                st.calls.append((method, path))
                st.complexity = body
                return self._send({})
            if path in ("/admin/v1/policies/lockout", "/admin/v1/policies/password/lockout"):
                if method == "GET":
                    return self._send({"policy": st.lockout})
                st.calls.append((method, path))
                st.lockout = body
                return self._send({})
            if path.startswith("/admin/v1/secretgenerators/"):
                kind = path.rsplit("/", 1)[1]
                if method == "GET":
                    return self._send({"secretGenerator": st.generators[kind]})
                st.calls.append((method, path))
                st.generators[kind] = body
                return self._send({})
            if path == "/admin/v1/languages/default":
                return self._send({"language": st.language})
            if path == "/admin/v1/languages/default/pt" and method == "PUT":
                st.calls.append((method, path))
                st.language = "pt"
                return self._send({})
            if path.startswith("/admin/v1/text/message/"):
                parts = path.split("/")
                kind, language = parts[-2], parts[-1]
                key = f"{kind}:{language}"
                if method == "GET":
                    if key not in st.texts:
                        return self._send({"code": 5}, 404)
                    return self._send({"customText": st.texts[key]})
                st.calls.append((method, path))
                st.texts[key] = body
                return self._send({})
            if path == "/admin/v1/email/_search":
                return self._send({"result": st.smtp})
            if path == "/admin/v1/email/smtp" and method == "POST":
                st.calls.append((method, path))
                password = body.get("plain", {}).get("password")
                st.smtp_password = password
                provider = {
                    "id": "smtp1", "state": "EMAIL_PROVIDER_STATE_INACTIVE",
                    "description": body.get("description", ""),
                    "smtp": {k: body[k] for k in
                             ("senderAddress", "senderName", "tls", "host", "user", "replyToAddress")},
                }
                st.smtp = [provider]
                return self._send({"details": {}, "id": "smtp1"})
            if path.startswith("/admin/v1/email/") and path.endswith("/_activate"):
                st.calls.append((method, path))
                st.smtp[0]["state"] = "EMAIL_PROVIDER_ACTIVE"
                return self._send({})
            if path.startswith("/admin/v1/email/smtp/") and path.endswith("/password"):
                st.calls.append((method, path))
                st.smtp_password = body.get("password")
                return self._send({})
            if path.startswith("/admin/v1/email/smtp/") and method == "PUT":
                st.calls.append((method, path))
                provider = st.smtp[0]
                provider["smtp"] = {k: body[k] for k in
                                    ("senderAddress", "senderName", "tls", "host", "user", "replyToAddress")}
                st.smtp_password = body.get("plain", {}).get("password", st.smtp_password)
                return self._send({})
            if path == "/management/v1/orgs/me":
                return self._send({"org": {"id": "org1"}})
            if path.endswith("/ListProjects"):
                return self._send({"projects": st.projects})
            if path.endswith("/CreateProject"):
                st.calls.append((method, path))
                st.projects.append({"projectId": "p1", "name": body["name"]})
                return self._send({"projectId": "p1"})
            if path.endswith("/ListApplications"):
                return self._send({"applications": st.apps})
            if path.endswith("/CreateApplication"):
                st.calls.append((method, path))
                st.apps.append({"applicationId": "a1", "name": body["name"],
                                "oidcConfiguration": {**body["oidcConfiguration"], "clientId": "cid"}})
                return self._send({"applicationId": "a1",
                                   "oidcConfiguration": {"clientId": "cid", "clientSecret": SECRET}})
            if path.endswith("/UpdateApplication"):
                st.calls.append((method, path))
                st.apps[0]["oidcConfiguration"] = {**body["oidcConfiguration"], "clientId": "cid"}
                return self._send({})
            if path.endswith("/GenerateClientSecret"):
                st.calls.append((method, path))
                return self._send({"clientSecret": SECRET + "-novo"})
            return self._send({"code": 5}, 404)

        def do_GET(self):
            self.handle_any("GET")

        def do_POST(self):
            self.handle_any("POST")

        def do_PUT(self):
            self.handle_any("PUT")

        def do_DELETE(self):
            self.handle_any("DELETE")

    return H


class ProvisionTest(unittest.TestCase):
    def setUp(self):
        self.state = FakeState()
        self.server = HTTPServer(("127.0.0.1", 0), make_handler(self.state))
        threading.Thread(target=self.server.serve_forever, daemon=True).start()
        self.addCleanup(self.server.server_close)
        self.addCleanup(self.server.shutdown)
        self.base = f"http://127.0.0.1:{self.server.server_port}"
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.env = Path(self.tmp.name) / ".env"
        self.env.write_text("# nota\nVALKEY_PASSWORD=abc\nOUTRA=1\nMAILPIT_SMTP_USERNAME=pitbox-smtp\nMAILPIT_SMTP_PASSWORD=segredo-smtp\n")

    def run_once(self, **kw):
        out = io.StringIO()
        with redirect_stdout(out):
            prov.run(self.base, "http://localhost:8081", PAT, self.env, kw.get("rotate", False),
                     log=print, rotate_smtp_password=kw.get("smtp_rotate", False))
        return out.getvalue()

    def test_first_run_reaches_the_desired_state(self):
        out = self.run_once()
        self.assertEqual(self.state.oidc, {k: f"{v}s" for k, v in prov.OIDC_SETTINGS.items()})
        self.assertEqual(self.state.factors, ["SECOND_FACTOR_TYPE_OTP"])
        self.assertEqual(self.state.login_policy["passwordCheckLifetime"], "86400s")
        for key in prov.LOGIN_POLICY_DURATIONS:
            self.assertTrue(self.state.login_policy[key].endswith("s"))
            self.assertNotEqual(self.state.login_policy[key], "0s")
        self.assertEqual(len(self.state.projects), 1)
        cfg = self.state.apps[0]["oidcConfiguration"]
        self.assertEqual(prov.config_drift(cfg, prov.desired_oidc_config("http://localhost:8081")), [])
        self.assertEqual(cfg["accessTokenType"], "OIDC_TOKEN_TYPE_BEARER")  # opaco
        self.assertEqual(self.state.webkeys[-1]["state"], "STATE_ACTIVE")
        self.assertIn("ecdsa", self.state.webkeys[-1])
        self.assertTrue(self.state.login_policy["allowRegister"])
        self.assertTrue(self.state.login_policy["ignoreUnknownUsernames"])
        self.assertFalse(self.state.login_policy["allowExternalIdp"])
        self.assertTrue(self.state.login_policy["disableLoginWithPhone"])
        self.assertEqual(self.state.login_policy["passwordlessType"], "PASSWORDLESS_TYPE_ALLOWED")
        self.assertEqual(self.state.complexity["minLength"], 12)
        self.assertFalse(any(self.state.complexity[k] for k in ("hasUppercase", "hasLowercase", "hasNumber", "hasSymbol")))
        self.assertEqual(self.state.lockout["maxPasswordAttempts"], 0)
        self.assertEqual(self.state.lockout["maxOtpAttempts"], 10)
        self.assertEqual(self.state.language, "pt")
        self.assertEqual(self.state.generators["SECRET_GENERATOR_TYPE_VERIFY_EMAIL_CODE"]["expiry"], "1800s")
        self.assertEqual(self.state.generators["SECRET_GENERATOR_TYPE_PASSWORD_RESET_CODE"]["expiry"], "900s")
        self.assertEqual(self.state.smtp[0]["state"], "EMAIL_PROVIDER_ACTIVE")
        self.assertEqual(self.state.smtp_password, "segredo-smtp")
        self.assertIn("verifyemail:pt", self.state.texts)
        self.assertIsNotNone(self.state.texts["verifyemail:pt"].get("text"))
        env = prov.parse_env(self.env.read_text())
        self.assertEqual(env["PITBOX_OIDC_CLIENT_ID"], "cid")
        self.assertEqual(env["PITBOX_OIDC_CLIENT_SECRET"], SECRET)
        self.assertEqual(env["VALKEY_PASSWORD"], "abc")  # o resto do ficheiro fica como estava
        self.assertIn("# nota", self.env.read_text())
        self.assertEqual(stat.S_IMODE(self.env.stat().st_mode), 0o600)
        for secret in (PAT, SECRET, "cid"):
            self.assertNotIn(secret, out, "o output não pode conter segredos nem o client_id")

    def test_second_run_changes_nothing(self):
        self.run_once()
        before = self.env.read_text()
        self.state.calls.clear()
        out = self.run_once()
        self.assertEqual(self.state.calls, [], "uma segunda execução não pode mudar nada")
        self.assertEqual(self.env.read_text(), before)
        self.assertNotIn(SECRET, out)

    def test_drift_is_corrected_without_rotating_the_secret(self):
        self.run_once()
        self.state.apps[0]["oidcConfiguration"]["accessTokenType"] = "OIDC_TOKEN_TYPE_BEARER"
        self.state.apps[0]["oidcConfiguration"]["redirectUris"] = ["http://evil.example/cb"]
        self.state.calls.clear()
        self.run_once()
        self.assertEqual([c[1].rsplit("/", 1)[1] for c in self.state.calls], ["UpdateApplication"])
        self.assertEqual(prov.parse_env(self.env.read_text())["PITBOX_OIDC_CLIENT_SECRET"], SECRET)

    def test_missing_secret_is_regenerated_and_rotation_is_explicit(self):
        self.run_once()
        self.env.write_text("OUTRA=1\nMAILPIT_SMTP_USERNAME=pitbox-smtp\nMAILPIT_SMTP_PASSWORD=segredo-smtp\n")
        self.run_once()
        self.assertEqual(prov.parse_env(self.env.read_text())["PITBOX_OIDC_CLIENT_SECRET"], SECRET + "-novo")

    def test_smtp_password_rotation_is_explicit(self):
        self.run_once()
        old = prov.parse_env(self.env.read_text())["MAILPIT_SMTP_PASSWORD"]
        self.state.calls.clear()
        out = self.run_once(smtp_rotate=True)
        new = prov.parse_env(self.env.read_text())["MAILPIT_SMTP_PASSWORD"]
        self.assertNotEqual(new, old)
        self.assertEqual(self.state.smtp_password, new)
        self.assertTrue(any(c[1].endswith("/password") for c in self.state.calls))
        self.assertNotIn(old, out)
        self.assertNotIn(new, out)

    def test_a_remote_host_is_refused(self):
        with self.assertRaises(prov.ProvisionError):
            prov.Zitadel("https://zitadel.example.com", PAT)

    def test_a_wrong_pat_fails_without_leaking_it(self):
        with self.assertRaises(prov.ProvisionError) as cm:
            out = io.StringIO()
            with redirect_stdout(out):
                prov.run(self.base, "http://localhost:8081", "pat-errado", self.env, False)
        self.assertNotIn("pat-errado", str(cm.exception))

    def test_pat_file_must_be_private(self):
        f = Path(self.tmp.name) / "pat"
        f.write_text(PAT)
        os.chmod(f, 0o644)
        with self.assertRaises(prov.ProvisionError):
            prov.read_pat_file(str(f))
        os.chmod(f, 0o600)
        self.assertEqual(prov.read_pat_file(str(f)), PAT)


class PureTest(unittest.TestCase):
    def test_update_env_replaces_and_appends(self):
        out = prov.update_env("A=1\nB=2\n", {"B": "3", "C": "4"})
        self.assertEqual(out, "A=1\nB=3\nC=4\n")

    def test_drift_reads_a_missing_enum_as_its_zero_value(self):
        want = {"applicationType": "OIDC_APP_TYPE_WEB", "accessTokenType": "OIDC_TOKEN_TYPE_JWT"}
        self.assertEqual(prov.config_drift({}, want), ["accessTokenType"])

    def test_the_id_token_carries_no_personal_data(self):
        self.assertIs(prov.desired_oidc_config("http://localhost:8081")["idTokenUserinfoAssertion"], False)

    def test_http_redirects_need_development_mode(self):
        self.assertTrue(prov.desired_oidc_config("http://localhost:8081")["developmentMode"])
        self.assertFalse(prov.desired_oidc_config("https://api.example.com")["developmentMode"])

    def test_dev_registers_both_local_origins(self):
        cfg = prov.desired_oidc_config("http://localhost:8081")
        self.assertEqual(
            cfg["redirectUris"],
            ["http://localhost:8081/auth/callback", "http://localhost:5173/auth/callback"],
        )
        self.assertEqual(
            cfg["postLogoutRedirectUris"],
            ["http://localhost:8081/auth/signed-out", "http://localhost:5173/auth/signed-out"],
        )
        self.assertEqual(cfg["backChannelLogoutUri"], "http://localhost:8081/auth/backchannel-logout")

    def test_an_extra_local_port_is_kept_first(self):
        cfg = prov.desired_oidc_config("http://localhost:9000")
        self.assertEqual(
            cfg["redirectUris"],
            [
                "http://localhost:9000/auth/callback",
                "http://localhost:8081/auth/callback",
                "http://localhost:5173/auth/callback",
            ],
        )

    def test_production_registers_only_its_origin(self):
        cfg = prov.desired_oidc_config("https://app.pitbox.pt")
        self.assertEqual(cfg["redirectUris"], ["https://app.pitbox.pt/auth/callback"])


if __name__ == "__main__":
    unittest.main()
