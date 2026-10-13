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
        self.env.write_text("# nota\nVALKEY_PASSWORD=abc\nOUTRA=1\n")

    def run_once(self, **kw):
        out = io.StringIO()
        with redirect_stdout(out):
            prov.run(self.base, "http://localhost:8081", PAT, self.env, kw.get("rotate", False), log=print)
        return out.getvalue()

    def test_first_run_reaches_the_desired_state(self):
        out = self.run_once()
        self.assertEqual(self.state.oidc, {k: f"{v}s" for k, v in prov.OIDC_SETTINGS.items()})
        self.assertEqual(self.state.factors, ["SECOND_FACTOR_TYPE_OTP"])
        self.assertEqual(len(self.state.projects), 1)
        cfg = self.state.apps[0]["oidcConfiguration"]
        self.assertEqual(prov.config_drift(cfg, prov.desired_oidc_config("http://localhost:8081")), [])
        self.assertEqual(cfg["accessTokenType"], "OIDC_TOKEN_TYPE_JWT")
        self.assertEqual(self.state.webkeys[-1]["state"], "STATE_ACTIVE")
        self.assertIn("ecdsa", self.state.webkeys[-1])
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
        self.env.write_text("OUTRA=1\n")
        self.run_once()
        self.assertEqual(prov.parse_env(self.env.read_text())["PITBOX_OIDC_CLIENT_SECRET"], SECRET + "-novo")

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


if __name__ == "__main__":
    unittest.main()
