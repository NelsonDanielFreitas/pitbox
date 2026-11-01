"""Testes da limpeza de identity.user_account do auth-smoke.py (sem Docker).

Correr: python3 -I -m unittest discover -s scripts -p 'test_*.py'
"""
import importlib.util
import unittest
from pathlib import Path

SPEC = importlib.util.spec_from_file_location("auth_smoke", Path(__file__).with_name("auth-smoke.py"))
smoke = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(smoke)


class CleanupUserAccounts(unittest.TestCase):
    def test_deletes_only_given_subjects(self):
        calls = []
        n = smoke.cleanup_user_accounts(lambda q: calls.append(q) or "2", ["123", "456", "123"])
        self.assertEqual(n, 2)
        self.assertEqual(len(calls), 1)
        self.assertIn("WHERE idp_subject IN ('123', '456')", calls[0])
        self.assertNotIn("email", calls[0])

    def test_empty_list_runs_no_sql(self):
        calls = []
        self.assertEqual(smoke.cleanup_user_accounts(calls.append, []), 0)
        self.assertEqual(calls, [])

    def test_invalid_subject_rejects_everything(self):
        for bad in ("1' OR '1'='1", "", "abc", "12 34", "1" * 33, None):
            calls = []
            with self.assertRaises(ValueError):
                smoke.cleanup_user_accounts(calls.append, ["123", bad])
            self.assertEqual(calls, [], f"não pode correr SQL com {bad!r}")


if __name__ == "__main__":
    unittest.main()
