"""Regressões de configuração do compose local que não se vêem num teste de código.

O login v2 do Zitadel só envia e exige o código de verificação quando
`EMAIL_VERIFICATION=true`. Sem isso, o registo cria uma conta não verificada, o BFF
recusa o login, e não há email para abrir. Este teste impede que a variável desapareça.
"""
import unittest
from pathlib import Path

COMPOSE = Path(__file__).resolve().parent.parent / "deploy" / "compose" / "docker-compose.yml"


class LoginEmailVerificationTest(unittest.TestCase):
    def test_login_v2_requires_email_verification(self):
        text = COMPOSE.read_text(encoding="utf-8")
        login = text.split("  zitadel-login:", 1)[1].split("  zitadel-proxy:", 1)[0]
        self.assertIn('EMAIL_VERIFICATION: "true"', login)

    def test_mailpit_ui_and_smtp_credentials_are_separate(self):
        text = COMPOSE.read_text(encoding="utf-8")
        mailpit = text.split("  mailpit:", 1)[1].split("  # ----------------------------------------------------------------- Zitadel", 1)[0]
        self.assertIn("MP_SMTP_AUTH:", mailpit)
        self.assertIn("MP_UI_AUTH:", mailpit)
        self.assertIn("127.0.0.1:${MAILPIT_UI_PORT:-8025}:8025", mailpit)


if __name__ == "__main__":
    unittest.main()
