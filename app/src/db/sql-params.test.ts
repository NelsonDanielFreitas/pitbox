import fc from "fast-check";
import { describe, expect, it } from "vitest";

import { bindValues, parseSql, SqlParamsError } from "./sql-params";

describe("parseSql", () => {
  it("turns each named parameter into ? in order, repeats included", () => {
    const parsed = parseSql("SELECT * FROM t WHERE a = $a AND b = :b OR a = $a AND c = @c");
    expect(parsed.sql).toBe("SELECT * FROM t WHERE a = ? AND b = ? OR a = ? AND c = ?");
    expect(parsed.names).toEqual(["$a", ":b", "$a", "@c"]);
  });

  it("leaves strings, quoted identifiers and comments alone", () => {
    const sql = [
      "SELECT '$in_string', 'it''s :still', \"$ident\", `:tick`, [@brackets], $real",
      "-- $line_comment :x",
      "/* $block_comment",
      "   @x */ FROM t WHERE x = :after",
    ].join("\n");
    const parsed = parseSql(sql);
    expect(parsed.names).toEqual(["$real", ":after"]);
    expect(parsed.sql).toContain("'$in_string'");
    expect(parsed.sql).toContain("'it''s :still'");
    expect(parsed.sql).toContain('"$ident"');
    expect(parsed.sql).toContain("-- $line_comment :x");
    expect(parsed.sql).toContain("/* $block_comment\n   @x */");
    expect(parsed.sql.match(/\?/g)).toHaveLength(2);
  });

  it("never reads a parameter inside a string, whatever the string holds (property)", () => {
    fc.assert(
      fc.property(fc.string({ unit: "grapheme-ascii" }), (text) => {
        const literal = `'${text.replaceAll("'", "''")}'`;
        const parsed = parseSql(`SELECT ${literal} AS v, $p`);
        expect(parsed.names).toEqual(["$p"]);
        expect(parsed.sql).toBe(`SELECT ${literal} AS v, ?`);
      }),
      { numRuns: 2000 },
    );
  });

  it("rejects positional parameters instead of guessing the order", () => {
    expect(() => parseSql("SELECT ? FROM t")).toThrow(SqlParamsError);
    expect(() => parseSql("SELECT ?1, $a FROM t")).toThrow(SqlParamsError);
  });

  it("counts statements, ignoring empty ones, comments and trailing semicolons", () => {
    expect(parseSql("SELECT 1").statements).toBe(1);
    expect(parseSql("SELECT 1;").statements).toBe(1);
    expect(parseSql("SELECT 1; ;; -- end\n").statements).toBe(1);
    expect(parseSql("SELECT 1; SELECT 2").statements).toBe(2);
    expect(parseSql("INSERT INTO t VALUES ('a;b'); SELECT 1").statements).toBe(2);
    expect(parseSql("-- only a comment").statements).toBe(0);
  });

  it("reports the first keyword and a top-level RETURNING", () => {
    expect(parseSql("  select 1").firstKeyword).toBe("select");
    expect(parseSql("\n-- c\nINSERT INTO t(a) VALUES ($a) RETURNING a")).toMatchObject({
      firstKeyword: "insert",
      returning: true,
    });
    expect(parseSql("UPDATE t SET note = 'returning' WHERE id = $id")).toMatchObject({
      firstKeyword: "update",
      returning: false,
    });
    expect(parseSql("WITH x AS (SELECT 1) SELECT * FROM x").firstKeyword).toBe("with");
    expect(parseSql("PRAGMA quick_check").firstKeyword).toBe("pragma");
  });

  it("copes with an unterminated string or comment without hanging", () => {
    expect(parseSql("SELECT 'oops $a").names).toEqual([]);
    expect(parseSql("SELECT 1 /* $a").names).toEqual([]);
    expect(parseSql("SELECT $").names).toEqual([]);
  });
});

describe("bindValues", () => {
  const parsed = parseSql("INSERT INTO t(a, b, c) VALUES ($a, $b, $a)");

  it("orders the values by position and repeats a reused name", () => {
    expect(bindValues(parsed, { $a: 1, $b: "x" })).toEqual([1, "x", 1]);
  });

  it("binds NULL for a missing name, like the web engine, and ignores extras", () => {
    expect(bindValues(parsed, { $a: 1, $unused: "nope" })).toEqual([1, null, 1]);
    expect(bindValues(parsed, undefined)).toEqual([null, null, null]);
    expect(bindValues(parseSql("SELECT 1"), { $a: 1 })).toEqual([]);
  });

  it("refuses binary values", () => {
    expect(() => bindValues(parsed, { $a: new Uint8Array([1]), $b: "x" })).toThrow(SqlParamsError);
  });
});
