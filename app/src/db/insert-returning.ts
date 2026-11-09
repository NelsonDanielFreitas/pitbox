/**
 * `@capacitor-community/sqlite` (Android) implements `INSERT … RETURNING` by itself, and badly:
 *
 *  - it recognises the clause only after `VALUES (…)`: an `INSERT … SELECT … RETURNING` reaches
 *    SQLite untouched and Android refuses it ("Queries can be performed using SQLiteDatabase query
 *    or rawQuery methods only"), which broke saving a setup version on a real phone;
 *  - it reads the row back with `rowid BETWEEN last_id_before + 1 AND last_id_after`, but the last
 *    id belongs to the whole connection. The app writes the sync-queue row first (another table),
 *    so when the new row's rowid is lower than that one nothing comes back, the transaction asks for
 *    a row and rolls back, and the screen says "conflicts with another record". It depends on the
 *    numbers, so it looked random (cars worked, engines did not).
 *
 * So the driver splits every such statement: it runs the INSERT without the clause and reads the row
 * back with `last_insert_rowid()` taken right after the insert (the guard `changes() > 0` keeps an
 * insert that wrote nothing, such as `INSERT OR IGNORE` or a guarded `SELECT`, from returning the
 * previous insert's row). Every statement of the app that takes this path inserts at most one row.
 *
 * Upserts (`ON CONFLICT … DO UPDATE`) are left to the plugin: when they update, SQLite does not
 * move `last_insert_rowid()`, so the read-back would return the wrong row.
 */
export interface SplitInsert {
  /** The INSERT without its RETURNING clause. */
  insert: string;
  /** Reads back the row just inserted; yields no row when the INSERT wrote nothing. */
  readBack: string;
}

const INSERT_INTO = /^\s*INSERT\s+(?:OR\s+(?:ROLLBACK|ABORT|REPLACE|FAIL|IGNORE)\s+)?INTO\s+("?)([A-Za-z_][A-Za-z0-9_]*)\1/i;

/** The index of the top-level (outside parentheses and quotes) word RETURNING, or -1. */
function topLevelReturning(sql: string): number {
  let depth = 0;
  let quote: string | undefined;
  let found = -1;
  for (let i = 0; i < sql.length; i++) {
    const ch = sql.charAt(i);
    if (quote) {
      if (ch === quote) quote = undefined;
      continue;
    }
    if (ch === "'" || ch === '"') quote = ch;
    else if (ch === "(") depth++;
    else if (ch === ")") depth--;
    else if (depth === 0 && /[Rr]/.test(ch) && /^returning\b/i.test(sql.slice(i, i + 10)) && !/[A-Za-z0-9_]/.test(sql.charAt(i - 1))) {
      found = i;
    }
  }
  return found;
}

/** Returns the split for an `INSERT … RETURNING …` (not an upsert), and undefined for anything else. */
export function splitInsertReturning(sql: string): SplitInsert | undefined {
  const table = INSERT_INTO.exec(sql);
  if (!table) return undefined;
  const at = topLevelReturning(sql);
  if (at < 0) return undefined;
  const head = sql.slice(0, at).trim();
  const columns = sql.slice(at + "returning".length).trim().replace(/;$/, "").trim();
  if (columns === "" || /\bON\s+CONFLICT\b/i.test(head)) return undefined;
  const name = table[2];
  if (name === undefined) return undefined;
  return {
    insert: head,
    readBack: `SELECT ${columns} FROM ${name} WHERE rowid = last_insert_rowid() AND changes() > 0`,
  };
}
