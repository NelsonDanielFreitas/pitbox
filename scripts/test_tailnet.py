import importlib.util
import tempfile
import unittest
from pathlib import Path

spec = importlib.util.spec_from_file_location("tailnet", Path(__file__).with_name("tailnet.py"))
tn = importlib.util.module_from_spec(spec)
spec.loader.exec_module(tn)


class ValidateHost(unittest.TestCase):
    def test_accepts_tailnet_names(self):
        for ok in ("pc.tail1234.ts.net", "portatil-wsl.tail9f9359.ts.net", "a.b.c.ts.net"):
            self.assertEqual(tn.validate_host(ok), ok)

    def test_rejects_everything_else(self):
        for bad in (
            "", " ", "http://pc.tail1.ts.net", "https://pc.tail1.ts.net", "pc.tail1.ts.net:8443",
            "pc.tail1.ts.net/", "pc.tail1.ts.net/x", "ts.net", ".ts.net", "pc.ts.net.evil.example",
            "pc.example.com", "localhost", "evilts.net", "PC.tail1.ts.net", "pc.tail1.ts.net ",
            "pc..ts.net", "-pc.tail1.ts.net", "pc_x.tail1.ts.net", "pc.tail1.ts.net\n", "a" * 300 + ".ts.net",
        ):
            with self.subTest(bad=bad), self.assertRaises(tn.TailnetError):
                tn.validate_host(bad)


class FromEnvFile(unittest.TestCase):
    def read(self, text):
        with tempfile.TemporaryDirectory() as d:
            p = Path(d) / ".env"
            p.write_text(text, encoding="utf-8")
            return tn.read_host_from_env(p)

    def test_missing_file_or_key_means_localhost_profile(self):
        self.assertEqual(tn.read_host_from_env(Path("/nonexistent/.env")), "")
        self.assertEqual(self.read("A=1\n"), "")
        self.assertEqual(self.read("PITBOX_TAILNET_HOST=\n"), "")

    def test_valid_and_invalid_values(self):
        self.assertEqual(self.read("A=1\nPITBOX_TAILNET_HOST=pc.tail1.ts.net\n"), "pc.tail1.ts.net")
        with self.assertRaises(tn.TailnetError):
            self.read("PITBOX_TAILNET_HOST=http://pc.tail1.ts.net\n")


class ComposeEnv(unittest.TestCase):
    def test_localhost_profile_is_empty(self):
        self.assertEqual(tn.compose_env(""), {})

    def test_phone_profile(self):
        env = tn.compose_env("pc.tail1.ts.net")
        self.assertEqual(env["PITBOX_ZITADEL_ORIGIN"], "https://pc.tail1.ts.net:8443")
        self.assertEqual(env["PITBOX_ZITADEL_LOGIN_HOST"], "pc.tail1.ts.net:8443")
        self.assertEqual(env["PITBOX_ZITADEL_EXTERNAL_SECURE"], "true")

    def test_cli_rejects_bad_value(self):
        self.assertEqual(tn.main(["check", "http://x.ts.net"]), 1)
        self.assertEqual(tn.main(["check", "pc.tail1.ts.net"]), 0)
        self.assertEqual(tn.main(["nope"]), 2)


if __name__ == "__main__":
    unittest.main()
