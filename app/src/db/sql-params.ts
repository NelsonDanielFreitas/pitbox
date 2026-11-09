import type { SqlParams } from "./protocol";

export type SqlValue = string | number | null;

export interface ParsedSql {
  /** The SQL with every named parameter replaced by `?`. */
  sql: string;
  /** The parameter names, in the order their `?` appear (a name used twice appears twice). */
  names: string[];
  /** How many statements the text holds (empty ones do not count). */
  statements: number;
  /** Lower-case first keyword, e.g. "select", "insert", "pragma". */
  firstKeyword: string;
  /** A top-level word RETURNING: the statement writes and also yields rows. */
  returning: boolean;
}

export class SqlParamsError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "SqlParamsError";
  }
}

const IDENT_START = /[A-Za-z_]/;
const IDENT = /[A-Za-z0-9_]/;
const NAME_PREFIX = new Set(["$", ":", "@"]);

/**
 * Rewrites SQLite named parameters (`$id`, `:id`, `@id`) to the positional `?` that the
 * Capacitor SQLite plugin needs, without touching string literals, quoted identifiers or
 * comments. It is a scanner, not a regex: `'$not_a_param'` and `-- :nor_this` stay as they are.
 *
 * Positional parameters in the source (`?`, `?1`) are rejected: the app only uses named ones, and
 * mixing both would make the order of the values ambiguous.
 */
export function parseSql(sql: string): ParsedSql {
  const names: string[] = [];
  let out = "";
  let statements = 0;
  let hasContent = false;
  let firstKeyword = "";
  let returning = false;
  let word = "";

  const endWord = () => {
    if (word === "") return;
    const lower = word.toLowerCase();
    if (firstKeyword === "") firstKeyword = lower;
    if (lower === "returning") returning = true;
    word = "";
  };
  const content = () => {
    hasContent = true;
  };

  let i = 0;
  while (i < sql.length) {
    const ch = sql.charAt(i);
    const next = sql.charAt(i + 1);

    // -- line comment
    if (ch === "-" && next === "-") {
      endWord();
      const end = sql.indexOf("\n", i);
      const stop = end === -1 ? sql.length : end;
      out += sql.slice(i, stop);
      i = stop;
      continue;
    }
    // /* block comment */ (an unterminated one runs to the end, as SQLite does)
    if (ch === "/" && next === "*") {
      endWord();
      const end = sql.indexOf("*/", i + 2);
      const stop = end === -1 ? sql.length : end + 2;
      out += sql.slice(i, stop);
      i = stop;
      continue;
    }
    // 'string', "identifier", `identifier`, [identifier]: copied verbatim
    if (ch === "'" || ch === '"' || ch === "`" || ch === "[") {
      endWord();
      content();
      const close = ch === "[" ? "]" : ch;
      let j = i + 1;
      for (;;) {
        const end = sql.indexOf(close, j);
        if (end === -1) {
          j = sql.length; // unterminated: let SQLite report it
          break;
        }
        // A doubled quote is an escaped quote (not for [..], which has no escape).
        if (close !== "]" && sql.charAt(end + 1) === close) {
          j = end + 2;
          continue;
        }
        j = end + 1;
        break;
      }
      out += sql.slice(i, j);
      i = j;
      continue;
    }
    if (ch === "?") {
      throw new SqlParamsError("positional parameters are not supported; use named ones");
    }
    if (NAME_PREFIX.has(ch) && IDENT.test(next)) {
      endWord();
      content();
      let j = i + 1;
      while (j < sql.length && IDENT.test(sql.charAt(j))) j += 1;
      names.push(sql.slice(i, j)); // keeps the prefix: the caller's keys are `$id`
      out += "?";
      i = j;
      continue;
    }
    if (ch === ";") {
      endWord();
      if (hasContent) statements += 1;
      hasContent = false;
      out += ch;
      i += 1;
      continue;
    }
    if (IDENT_START.test(ch) || (word !== "" && IDENT.test(ch))) {
      word += ch;
      content();
    } else {
      endWord();
      if (!/\s/.test(ch)) content();
    }
    out += ch;
    i += 1;
  }
  endWord();
  if (hasContent) statements += 1;

  return { sql: out, names, statements, firstKeyword, returning };
}

/**
 * The values for the `?` of a parsed statement. Like the web engine, a name the caller did not
 * give binds NULL, and a given name the SQL never mentions is ignored. A binary value is
 * refused: nothing in the app stores BLOBs, and the plugin bridge is JSON.
 */
export function bindValues(parsed: ParsedSql, params: SqlParams | undefined): SqlValue[] {
  return parsed.names.map((name) => {
    const value = params?.[name];
    if (value === undefined) return null;
    if (value instanceof Uint8Array) {
      throw new SqlParamsError("binary parameters are not supported on the native engine");
    }
    return value;
  });
}
