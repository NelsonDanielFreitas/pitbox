import { describe, expect, it } from "vitest";

import { splitInsertReturning } from "./insert-returning";

describe("splitInsertReturning", () => {
  it("splits the setup-version statement that failed on the phone (INSERT … SELECT)", () => {
    const sql = `INSERT INTO setup_version (
                    team_id, setup_id, version
                  )
                  SELECT s.team_id, s.id, ?
                    FROM setup s
                   WHERE s.team_id = ? AND NOT EXISTS (SELECT 1 FROM setup_version x WHERE x.version = ?)
                  RETURNING
  setup_id, version, data`;
    const split = splitInsertReturning(sql);
    expect(split?.insert).not.toMatch(/returning/i);
    expect(split?.insert).toMatch(/NOT EXISTS \(SELECT 1 FROM setup_version x WHERE x.version = \?\)$/);
    expect(split?.readBack).toBe(
      "SELECT setup_id, version, data FROM setup_version WHERE rowid = last_insert_rowid() AND changes() > 0",
    );
  });

  it("splits INSERT … VALUES too: the plugin's own read-back loses the row of an engine", () => {
    const split = splitInsertReturning(
      "INSERT INTO engine (id, make) VALUES (?, ?)\n RETURNING \n id, make, version\n",
    );
    expect(split?.insert).toBe("INSERT INTO engine (id, make) VALUES (?, ?)");
    expect(split?.readBack).toBe(
      "SELECT id, make, version FROM engine WHERE rowid = last_insert_rowid() AND changes() > 0",
    );
    expect(splitInsertReturning("INSERT INTO t (a) VALUES ((SELECT 1)) RETURNING a")?.insert).toBe(
      "INSERT INTO t (a) VALUES ((SELECT 1))",
    );
  });

  it("leaves upserts, other statements and statements without RETURNING alone", () => {
    expect(
      splitInsertReturning("INSERT INTO t (a) VALUES (?) ON CONFLICT (a) DO UPDATE SET a = 1 RETURNING a"),
    ).toBeUndefined();
    expect(splitInsertReturning("UPDATE t SET a = 1 RETURNING a")).toBeUndefined();
    expect(splitInsertReturning("DELETE FROM t RETURNING a")).toBeUndefined();
    expect(splitInsertReturning("INSERT INTO t (a) SELECT 1")).toBeUndefined();
    expect(splitInsertReturning("INSERT INTO t (a) SELECT 1 RETURNING")).toBeUndefined();
  });

  it("ignores the word returning inside strings, names and parentheses", () => {
    expect(splitInsertReturning("INSERT INTO t (a) SELECT 'returning' FROM x")).toBeUndefined();
    expect(splitInsertReturning("INSERT INTO t (a) SELECT a_returning FROM x")).toBeUndefined();
    const split = splitInsertReturning("INSERT OR IGNORE INTO t (a) SELECT (SELECT 1 RETURNING_X) FROM x RETURNING a, b");
    expect(split?.readBack).toBe("SELECT a, b FROM t WHERE rowid = last_insert_rowid() AND changes() > 0");
  });
});
