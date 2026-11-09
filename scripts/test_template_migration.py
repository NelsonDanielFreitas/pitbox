import importlib.util
import json
import tempfile
import unittest
from pathlib import Path

spec = importlib.util.spec_from_file_location("tm", Path(__file__).with_name("template-migration.py"))
tm = importlib.util.module_from_spec(spec)
spec.loader.exec_module(tm)

GOOD = {
    "format": 1, "id": "01a1990b-b4dd-777f-9c5b-e31aaf013560", "slug": "mugen-mbx8r", "version": 1,
    "make": "Mugen Seiki", "model": "MBX8R", "scale": "1/8", "powertrain": "nitro",
    "name": {"pt": "Mugen MBX8R (O'Neil)"}, "groups": [],
}


class TemplateMigrationTest(unittest.TestCase):
    def write(self, d, name, data):
        p = Path(d) / name
        p.write_text(data if isinstance(data, str) else json.dumps(data), encoding="utf-8")
        return p

    def test_inserts_escape_quotes_and_bump_only_forward(self):
        with tempfile.TemporaryDirectory() as d:
            sql = tm.inserts(tm.load(self.write(d, "mugen-mbx8r.v1.json", GOOD)))
        self.assertIn("'Mugen MBX8R (O''Neil)'", sql)
        self.assertIn("current_version < EXCLUDED.current_version", sql)
        self.assertIn("::jsonb", sql)

    def test_dollar_tag_never_appears_in_the_payload(self):
        data = dict(GOOD, name={"pt": "x $tpl$ y"})
        with tempfile.TemporaryDirectory() as d:
            t = tm.load(self.write(d, "mugen-mbx8r.v1.json", data))
        tag = tm.tag_for(t["_raw"])
        self.assertNotEqual(tag, "$tpl$")
        self.assertEqual(t["_raw"].count(tag), 0)

    def test_rejections(self):
        cases = {
            "bad name.json": GOOD,
            "mugen-mbx8r.v2.json": GOOD,  # version differs from the name
            "other.v1.json": GOOD,        # slug differs from the name
            "mugen-mbx8r.v1.json": dict(GOOD, id="01A1990B-B4DD-777F-9C5B-E31AAF013560"),
        }
        with tempfile.TemporaryDirectory() as d:
            for name, data in cases.items():
                with self.assertRaises(tm.TemplateError, msg=name):
                    tm.load(self.write(d, name, data))
            dup = '{"id": "x", "id": "y"}'
            with self.assertRaises(tm.TemplateError):
                tm.load(self.write(d, "dup-one.v1.json", dup))

    def test_same_id_for_two_slugs_is_refused(self):
        with tempfile.TemporaryDirectory() as d:
            self.write(d, "mugen-mbx8r.v1.json", GOOD)
            self.write(d, "copy-cat.v1.json", dict(GOOD, slug="copy-cat"))
            with self.assertRaises(tm.TemplateError):
                tm.all_templates(Path(d))

    def test_drift_sql_compares_jsonb(self):
        with tempfile.TemporaryDirectory() as d:
            sql = tm.drift_sql(self.write(d, "mugen-mbx8r.v1.json", GOOD))
        self.assertIn("ASSERT", sql)
        self.assertIn("definition = ", sql)

    def test_repository_templates_load(self):
        root = Path(__file__).resolve().parent.parent / "backend/internal/setup/templates"
        self.assertTrue(tm.all_templates(root))


if __name__ == "__main__":
    unittest.main()
