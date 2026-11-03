#!/usr/bin/env python3
"""Gera o SQL do catálogo global de modelos de ficha (F2-00, ADR 0030).

Os ficheiros backend/internal/setup/templates/<slug>.v<N>.json são a fonte; o validador
estrito é o do Go (domain.ParseTemplate, testado sobre cada ficheiro). Aqui só se lê o
suficiente para escrever os INSERT, e recusa-se tudo o que pareça um erro de cópia.

Uso:
  template-migration.py FICHEIRO --out DIR      escreve uma migração nova (timestamp_template-<slug>-v<N>.sql)
  template-migration.py --schema SCHEMA.sql     reescreve o bloco "catálogo de modelos" do schema.sql
  template-migration.py --drift-sql FICHEIRO    SQL que falha se a linha da base diferir do ficheiro

Sem dependências fora da biblioteca padrão.
"""
import argparse
import datetime
import json
import re
import sys
import uuid
from pathlib import Path

TEMPLATES = Path("backend/internal/setup/templates")
NAME = re.compile(r"^(?P<slug>[a-z0-9][a-z0-9-]{1,62})\.v(?P<ver>[1-9][0-9]{0,8})\.json$")
BEGIN = "-- BEGIN catálogo de modelos (gerado por scripts/template-migration.py; não editar)"
END = "-- END catálogo de modelos"


class TemplateError(Exception):
    pass


def reject_duplicates(pairs):
    keys = [k for k, _ in pairs]
    if len(keys) != len(set(keys)):
        raise TemplateError("chave JSON duplicada")
    return dict(pairs)


def load(path: Path) -> dict:
    m = NAME.match(path.name)
    if not m:
        raise TemplateError(f"{path.name}: o nome tem de ser <slug>.v<N>.json")
    raw = path.read_text(encoding="utf-8")
    if len(raw.encode()) > 256 << 10:
        raise TemplateError(f"{path.name}: acima de 256 KiB")
    try:
        t = json.loads(raw, object_pairs_hook=reject_duplicates)
    except (json.JSONDecodeError, TemplateError) as e:
        raise TemplateError(f"{path.name}: JSON inválido ({e})")
    if not isinstance(t, dict):
        raise TemplateError(f"{path.name}: o modelo tem de ser um objeto")
    if t.get("slug") != m["slug"] or t.get("version") != int(m["ver"]):
        raise TemplateError(f"{path.name}: slug/version do ficheiro não coincidem com o nome")
    try:
        if str(uuid.UUID(t["id"])) != t["id"] or t["id"] == str(uuid.UUID(int=0)):
            raise ValueError
    except (KeyError, ValueError, TypeError, AttributeError):
        raise TemplateError(f"{path.name}: id tem de ser um UUID minúsculo não nulo")
    for k in ("make", "model", "scale", "powertrain"):
        if not isinstance(t.get(k), str) or not t[k]:
            raise TemplateError(f"{path.name}: falta {k}")
    if not isinstance(t.get("name"), dict) or not isinstance(t["name"].get("pt"), str):
        raise TemplateError(f"{path.name}: falta name.pt")
    # O SQL leva o JSON compacto (jsonb ignora espaços e ordem); o ficheiro continua a ser a fonte revista.
    t["_raw"] = json.dumps(t, ensure_ascii=False, separators=(",", ":"))
    return t


def lit(v):
    if v is None:
        return "NULL"
    return "'" + str(v).replace("'", "''") + "'"


def tag_for(raw: str) -> str:
    tag = "tpl"
    while f"${tag}$" in raw:
        tag += "x"
    return f"${tag}$"


def inserts(t: dict) -> str:
    """O modelo é criado (ou, numa versão nova, só avança current_version/name/source_url)
    e a versão é acrescentada. A FK de current_version é diferida: tudo na mesma transação."""
    q = tag_for(t["_raw"])
    return f"""INSERT INTO setup.sheet_template (id, team_id, slug, make, model, scale, powertrain, name, source_url, current_version)
VALUES ({lit(t['id'])}, NULL, {lit(t['slug'])}, {lit(t['make'])}, {lit(t['model'])}, {lit(t['scale'])}, {lit(t['powertrain'])},
        {lit(t['name']['pt'])}, {lit(t.get('source_url'))}, {t['version']})
ON CONFLICT (id) DO UPDATE
  SET current_version = EXCLUDED.current_version, name = EXCLUDED.name, source_url = EXCLUDED.source_url
  WHERE setup.sheet_template.current_version < EXCLUDED.current_version;
INSERT INTO setup.sheet_template_version (template_id, version, definition)
VALUES ({lit(t['id'])}, {t['version']}, {q}{t['_raw'].strip()}{q}::jsonb);
"""


def all_templates(root: Path = TEMPLATES) -> list[dict]:
    ts = [load(p) for p in sorted(root.glob("*.json"))]
    seen = {}
    for t in ts:
        other = seen.setdefault(t["id"], t["slug"])
        if other != t["slug"]:
            raise TemplateError(f"o id {t['id']} é usado por {other} e {t['slug']}")
    return sorted(ts, key=lambda t: (t["slug"], t["version"]))


def write_migration(path: Path, out: Path) -> Path:
    t = load(path)
    stamp = datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%d%H%M%S")
    dest = out / f"{stamp}_template-{t['slug']}-v{t['version']}.sql"
    header = f"-- Gerado por scripts/template-migration.py a partir de {path.name}. Catálogo global: só leitura para a app.\n"
    dest.write_text(header + inserts(t), encoding="utf-8")
    return dest


def schema_block() -> str:
    body = "\n".join(inserts(t) for t in all_templates())
    return f"{BEGIN}\n{body}{END}\n"


def rewrite_schema(schema: Path) -> None:
    text = schema.read_text(encoding="utf-8")
    block = schema_block()
    if BEGIN in text:
        pattern = re.compile(re.escape(BEGIN) + r".*?" + re.escape(END) + r"\n", re.S)
        text = pattern.sub(lambda _: block, text, count=1)
    else:
        raise TemplateError("o schema.sql não tem o bloco 'catálogo de modelos' (acrescenta as duas linhas BEGIN/END)")
    schema.write_text(text, encoding="utf-8")


def drift_sql(path: Path) -> str:
    """SQL com uma asserção: a linha da base tem de ser igual ao ficheiro (jsonb, não texto)."""
    t = load(path)
    q = tag_for(t["_raw"])
    return f"""DO $drift$ BEGIN
  ASSERT (SELECT definition = {q}{t['_raw'].strip()}{q}::jsonb FROM setup.sheet_template_version
           WHERE template_id = {lit(t['id'])} AND version = {t['version']}),
    'modelo {t['slug']} v{t['version']}: a base difere do ficheiro (versão publicada alterada em silêncio, ou migração em falta)';
END $drift$;
"""


def main(argv=None) -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("file", nargs="?")
    ap.add_argument("--out")
    ap.add_argument("--schema")
    ap.add_argument("--drift-sql", action="store_true")
    a = ap.parse_args(argv)
    try:
        if a.schema:
            rewrite_schema(Path(a.schema))
        elif a.drift_sql and a.file:
            sys.stdout.write(drift_sql(Path(a.file)))
        elif a.file and a.out:
            print(write_migration(Path(a.file), Path(a.out)))
        else:
            ap.error("indica FICHEIRO --out DIR, --schema SCHEMA.sql ou --drift-sql FICHEIRO")
    except TemplateError as e:
        print(f"template-migration: {e}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
