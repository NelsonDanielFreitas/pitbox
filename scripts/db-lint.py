#!/usr/bin/env python3
"""Lint das migrações novas (o `atlas migrate lint` é Pro; ADR 0010).

Só olha para os ficheiros *.sql de backend/db/migrations acrescentados em relação ao
branch base (por omissão `main`), porque as migrações já aplicadas nunca mudam.
Falha em operações destrutivas ou bloqueantes. Exceção explícita e revista no PR:
`-- lint:allow <motivo>` na mesma linha ou na anterior.

Uso: python3 -I scripts/db-lint.py [base]   (base por omissão: main)
     python3 -I scripts/db-lint.py --files a.sql b.sql   (testes)
"""
import re
import subprocess
import sys
from pathlib import Path

MIGRATIONS = "backend/db/migrations"

RULES = [
    (r"\bDROP\b", "DROP é destrutivo: usar expand/contract (remover só depois de o código deixar de usar)"),
    (r"\bTRUNCATE\b", "TRUNCATE apaga dados"),
    (r"\bALTER\s+TABLE\b[^;]*?\bALTER\s+(?:COLUMN\s+)?\w+\s+(?:SET\s+DATA\s+)?TYPE\b", "ALTER ... TYPE reescreve a tabela e pode perder dados"),
    (r"\bRENAME\b", "RENAME parte o código que ainda usa o nome antigo"),
    (r"\bSET\s+NOT\s+NULL\b", "SET NOT NULL varre a tabela com lock: acrescentar CHECK ... NOT VALID e validar antes"),
]
INDEX = re.compile(r"\bCREATE\s+(?:UNIQUE\s+)?INDEX\s+(?:IF\s+NOT\s+EXISTS\s+)?(?P<name>\S+)?\s+ON\s+(?:ONLY\s+)?(?P<table>[\w.\"]+)", re.I)


def strip_comments(text: str) -> str:
    """Troca comentários por espaços, preservando posições (e por isso os números de linha)."""
    return re.sub(r"--[^\n]*", lambda m: " " * len(m.group(0)), text)


def lint(path: Path) -> list[str]:
    raw = path.read_text()
    lines = raw.split("\n")
    code = strip_comments(raw)
    problems = []

    def allowed(lineno: int) -> bool:
        return any("lint:allow" in lines[i] for i in (lineno - 1, lineno - 2) if 0 <= i < len(lines))

    def report(pos: int, msg: str):
        lineno = raw.count("\n", 0, pos) + 1
        if not allowed(lineno):
            problems.append(f"{path}:{lineno}: {msg}")

    for pattern, msg in RULES:
        for m in re.finditer(pattern, code, re.I | re.S):
            report(m.start(), msg)

    for m in INDEX.finditer(code):
        stmt_end = code.find(";", m.start())
        stmt = code[m.start(): stmt_end if stmt_end != -1 else len(code)]
        table = m.group("table").replace('"', "")
        created_here = re.search(rf"\bCREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?{re.escape(table)}\b", code, re.I)
        if not re.search(r"\bCONCURRENTLY\b", stmt, re.I) and not created_here:
            report(m.start(), f"CREATE INDEX em tabela existente ({table}) sem CONCURRENTLY bloqueia escritas")
    return problems


def new_files(base: str) -> list[Path]:
    out = subprocess.run(
        ["git", "diff", "--name-only", "--diff-filter=A", f"{base}...HEAD", "--", MIGRATIONS],
        check=True, capture_output=True, text=True,
    ).stdout.split()
    # Ficheiros ainda não commitados (trabalho local) também contam.
    out += subprocess.run(
        ["git", "ls-files", "--others", "--exclude-standard", "--", MIGRATIONS],
        check=True, capture_output=True, text=True,
    ).stdout.split()
    out += subprocess.run(
        ["git", "diff", "--name-only", "--diff-filter=A", "HEAD", "--", MIGRATIONS],
        check=True, capture_output=True, text=True,
    ).stdout.split()
    return sorted({Path(f) for f in out if f.endswith(".sql")})


def main() -> int:
    args = sys.argv[1:]
    if args[:1] == ["--files"]:
        files = [Path(a) for a in args[1:]]
    else:
        files = new_files(args[0] if args else "main")
    problems = [p for f in files for p in lint(f)]
    for p in problems:
        print(p)
    print(f"db-lint: {len(files)} migração(ões) nova(s), {len(problems)} problema(s)")
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
