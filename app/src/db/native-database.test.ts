import { beforeEach, describe, expect, it } from "vitest";

import { MemoryNativeDriver } from "@/test/native-driver";

import {
  CorruptLocalDataError,
  LocalConstraintError,
  LocalDatabaseUnavailableError,
} from "./errors";
import { migrations } from "./migrations";
import type { LogoutMarks } from "./logout-marks";
import { NativeDatabase, toLocalError } from "./native-database";
import { hashUserId } from "./user-hash";

class MemoryMarks implements LogoutMarks {
  readonly hashes = new Set<string>();
  list = () => Promise.resolve([...this.hashes]);
  add = (hash: string) => Promise.resolve(void this.hashes.add(hash));
  remove = (hash: string) => Promise.resolve(void this.hashes.delete(hash));
}

const ALICE = "user-alice";
const BOB = "user-bob";
const nameOf = async (userId: string) => `pitbox_${await hashUserId(userId)}`;

let driver: MemoryNativeDriver;
let marks: MemoryMarks;
let db: NativeDatabase;

beforeEach(async () => {
  driver = await MemoryNativeDriver.create();
  marks = new MemoryMarks();
  db = new NativeDatabase(driver, marks);
});

async function put(key: string, value: string): Promise<void> {
  await db.transaction([
    {
      sql: "INSERT INTO app_meta (key, value) VALUES ($key, $value)",
      params: { $key: key, $value: value },
    },
  ]);
}

describe("open", () => {
  it("runs every migration of the web engine, once, and keeps them across reopens", async () => {
    await db.open(ALICE);
    const count = () =>
      db.query<{ n: number }>("SELECT count(*) AS n FROM schema_migrations").then((r) => r[0]?.n);
    expect(await count()).toBe(migrations.length);
    const tables = await db.query<{ name: string }>(
      "SELECT name FROM sqlite_master WHERE type = 'table' AND name IN ('app_meta', 'sync_outbox')",
    );
    expect(tables.map((t) => t.name).sort()).toEqual(["app_meta", "sync_outbox"]);

    const appliedAt = () =>
      db.query<{ applied_at: string }>("SELECT applied_at FROM schema_migrations ORDER BY version");
    const before = await appliedAt();
    await put("survives", "reopen");

    await db.close();
    await db.open(ALICE);
    expect(await count()).toBe(migrations.length);
    // The same rows, not re-applied on an empty file: the data and the timestamps survived.
    expect(await appliedAt()).toEqual(before);
    expect(await db.query("SELECT 1 FROM app_meta WHERE key = 'survives'")).toHaveLength(1);
  });

  it("applies the pragmas on every open (foreign keys are per connection)", async () => {
    await db.open(ALICE);
    expect((await db.query<{ foreign_keys: number }>("PRAGMA foreign_keys"))[0]?.foreign_keys).toBe(1);
    expect((await db.query<{ trusted_schema: number }>("PRAGMA trusted_schema"))[0]?.trusted_schema).toBe(0);
    await db.close();
    await db.open(ALICE);
    expect((await db.query<{ foreign_keys: number }>("PRAGMA foreign_keys"))[0]?.foreign_keys).toBe(1);
  });

  it("creates the passphrase once and never asks for another", async () => {
    await db.open(ALICE);
    await db.close();
    await db.open(BOB);
    expect(driver.secretSets).toBe(1);
  });

  it("is a no-op when the same user is already open", async () => {
    await db.open(ALICE);
    await put("k", "v");
    await db.open(ALICE);
    expect(await db.query("SELECT 1 FROM app_meta WHERE key = 'k'")).toHaveLength(1);
  });

  it("keeps each user's data apart", async () => {
    await db.open(ALICE);
    await put("who", "alice");
    await db.open(BOB);
    expect(await db.query("SELECT * FROM app_meta WHERE key = 'who'")).toHaveLength(0);
    await db.open(ALICE);
    expect((await db.query<{ value: string }>("SELECT value FROM app_meta WHERE key = 'who'"))[0]?.value).toBe(
      "alice",
    );
  });
});

describe("fails closed", () => {
  it("refuses everything before open", async () => {
    await expect(db.query("SELECT 1")).rejects.toBeInstanceOf(LocalDatabaseUnavailableError);
    await expect(db.exec("SELECT 1")).rejects.toBeInstanceOf(LocalDatabaseUnavailableError);
    await expect(db.transaction([{ sql: "SELECT 1" }])).rejects.toBeInstanceOf(
      LocalDatabaseUnavailableError,
    );
  });

  it("never uses a file that is not encrypted, and leaves it alone", async () => {
    const name = await nameOf(ALICE);
    await driver.ensureSecret();
    driver.files.set(name, { secret: "x", encrypted: false, generation: 99 });
    await expect(db.open(ALICE)).rejects.toBeInstanceOf(CorruptLocalDataError);
    expect(driver.files.has(name)).toBe(true); // not deleted, not replaced
    await expect(db.query("SELECT 1")).rejects.toBeInstanceOf(LocalDatabaseUnavailableError);
  });

  it("does not replace a database it cannot open with an empty one (wrong key)", async () => {
    await db.open(ALICE);
    await put("precious", "data");
    await db.close();
    driver.secret = "another-passphrase";

    await expect(db.open(ALICE)).rejects.toBeInstanceOf(CorruptLocalDataError);
    expect(await driver.exists(await nameOf(ALICE))).toBe(true);

    driver.secret = "secret-1"; // the right one is back: the data is still there
    await db.open(ALICE);
    expect((await db.query<{ value: string }>("SELECT value FROM app_meta WHERE key = 'precious'"))[0]?.value).toBe(
      "data",
    );
  });

  it("reports a failed quick_check as corrupt", async () => {
    await db.open(ALICE);
    await db.close();
    const broken = Object.create(driver) as MemoryNativeDriver;
    broken.read = (sql, values) =>
      sql === "PRAGMA quick_check"
        ? Promise.resolve([{ quick_check: "row 1 missing from index" }])
        : driver.read(sql, values);
    await expect(new NativeDatabase(broken, marks).open(ALICE)).rejects.toBeInstanceOf(
      CorruptLocalDataError,
    );
  });
});

describe("statements", () => {
  beforeEach(async () => {
    await db.open(ALICE);
  });

  it("binds named parameters, reuses a name and binds NULL for a missing one", async () => {
    const rows = await db.query<{ a: number; b: string | null; c: number }>(
      "SELECT $x AS a, $missing AS b, $x AS c",
      { $x: 7 },
    );
    expect(rows).toEqual([{ a: 7, b: null, c: 7 }]);
  });

  it("does not read a parameter out of a string", async () => {
    const rows = await db.query<{ v: string; w: number }>("SELECT '$x :y' AS v, $x AS w", { $x: 5 });
    expect(rows).toEqual([{ v: "$x :y", w: 5 }]);
  });

  it("returns the rows of INSERT ... RETURNING, in a transaction and with requireRows", async () => {
    const rows = await db.transaction<{ key: string; value: string }>(
      [
        {
          sql: "INSERT INTO app_meta (key, value) VALUES ($key, $value) RETURNING key, value",
          params: { $key: "a", $value: "1" },
        },
      ],
      { requireRows: true },
    );
    expect(rows).toEqual([{ key: "a", value: "1" }]);
  });

  it("returns the rows of the LAST statement of a transaction", async () => {
    await put("a", "1");
    const rows = await db.transaction<{ value: string }>([
      { sql: "UPDATE app_meta SET value = $v WHERE key = 'a'", params: { $v: "2" } },
      { sql: "SELECT value FROM app_meta WHERE key = 'a'" },
    ]);
    expect(rows).toEqual([{ value: "2" }]);
  });

  it("is all or nothing: a failing statement undoes the ones before it", async () => {
    await put("a", "1");
    await expect(
      db.transaction([
        { sql: "INSERT INTO app_meta (key, value) VALUES ('b', '2')" },
        { sql: "INSERT INTO app_meta (key, value) VALUES ('a', 'dup')" }, // primary key clash
      ]),
    ).rejects.toBeInstanceOf(LocalConstraintError);
    expect(await db.query("SELECT 1 FROM app_meta WHERE key = 'b'")).toHaveLength(0);
    // and the connection is usable again
    await put("c", "3");
  });

  it("rolls back and reports a constraint when requireRows gets nothing", async () => {
    await expect(
      db.transaction(
        [
          { sql: "INSERT INTO app_meta (key, value) VALUES ('z', '1')" },
          { sql: "SELECT value FROM app_meta WHERE key = 'nope'" },
        ],
        { requireRows: true },
      ),
    ).rejects.toBeInstanceOf(LocalConstraintError);
    expect(await db.query("SELECT 1 FROM app_meta WHERE key = 'z'")).toHaveLength(0);
  });

  it("runs a parameterless multi-statement script", async () => {
    await db.exec("CREATE TABLE t1 (a INTEGER); INSERT INTO t1 VALUES (1); INSERT INTO t1 VALUES (2);");
    expect(await db.query("SELECT a FROM t1")).toHaveLength(2);
  });

  it("refuses parameters in a multi-statement script and binary parameters", async () => {
    await expect(db.exec("SELECT $a; SELECT $a", { $a: 1 })).rejects.toBeInstanceOf(
      LocalDatabaseUnavailableError,
    );
    await expect(db.query("SELECT $a", { $a: new Uint8Array([1]) })).rejects.toBeInstanceOf(
      LocalDatabaseUnavailableError,
    );
    await expect(db.query("SELECT ?", {})).rejects.toBeInstanceOf(LocalDatabaseUnavailableError);
  });

  it("runs concurrent transactions one at a time (no interleaving)", async () => {
    await put("n", "0");
    await Promise.all(
      Array.from({ length: 25 }, () =>
        db.transaction([
          {
            sql: "UPDATE app_meta SET value = CAST(CAST(value AS INTEGER) + 1 AS TEXT) WHERE key = 'n' RETURNING value",
          },
        ]),
      ),
    );
    expect((await db.query<{ value: string }>("SELECT value FROM app_meta WHERE key = 'n'"))[0]?.value).toBe("25");
  });

  it("keeps working after a failed statement", async () => {
    await expect(db.query("SELECT * FROM no_such_table")).rejects.toBeInstanceOf(
      LocalDatabaseUnavailableError,
    );
    expect(await db.query("SELECT 1 AS ok")).toEqual([{ ok: 1 }]);
  });
});

describe("errors", () => {
  it("do not carry the engine's text (it can include SQL), but keep it as the cause", () => {
    const raw = new Error("UNIQUE constraint failed: app_meta.key (INSERT INTO app_meta VALUES 'secret')");
    const mapped = toLocalError(raw);
    expect(mapped).toBeInstanceOf(LocalConstraintError);
    expect(mapped.message).not.toMatch(/INSERT|secret|app_meta/);
    expect(mapped.cause).toBe(raw);
  });

  it("keeps SQLite's constraint text (object names only) in `detail`, never in the message", () => {
    const raw = new Error("Run: UNIQUE constraint failed: tyre_set.team_id, tyre_set.label (code 2067)");
    const mapped = toLocalError(raw);
    expect(mapped).toBeInstanceOf(LocalConstraintError);
    expect((mapped as LocalConstraintError).detail).toBe("UNIQUE constraint failed: tyre_set.team_id, tyre_set.label");
    expect(mapped.message).toBe("local data constraint failed");
  });

  it("drops SQL text and values from the detail", () => {
    const raw = new Error("constraint violation near INSERT INTO car VALUES ('secret')");
    expect((toLocalError(raw) as LocalConstraintError).detail).toBeUndefined();
    const withSql = new Error("NOT NULL constraint failed: car.make (INSERT INTO car VALUES ('secret'))");
    expect((toLocalError(withSql) as LocalConstraintError).detail).toBe("NOT NULL constraint failed: car.make");
  });

  it.each([
    ["database is locked", "LocalDatabaseBusyError"],
    ["file is not a database", "CorruptLocalDataError"],
    ["file is encrypted or is not a database", "CorruptLocalDataError"],
    ["something else entirely", "LocalDatabaseUnavailableError"],
  ])("classifies %s", (text, name) => {
    expect(toLocalError(new Error(text)).name).toBe(name);
    expect(toLocalError(text).name).toBe(name);
  });
});

describe("logout and delete", () => {
  it("exists() reports without ever creating a database", async () => {
    expect(await db.exists("nobody")).toBe(false);
    expect(driver.files.size).toBe(0);
    await db.open(ALICE);
    expect(await db.exists(ALICE)).toBe(true);
  });

  it("deletes the user's database and forgets the passphrase when it was the last one", async () => {
    await db.open(ALICE);
    await put("k", "v");
    await db.markLogout(ALICE);
    expect(marks.hashes.size).toBe(1);

    await db.deleteUserDatabase(ALICE);
    expect(driver.files.size).toBe(0);
    expect(marks.hashes.size).toBe(0);
    expect(driver.secret).toBeUndefined();
    await expect(db.query("SELECT 1")).rejects.toBeInstanceOf(LocalDatabaseUnavailableError);

    await db.open(ALICE); // a fresh start works, with a new passphrase
    expect(await db.query("SELECT 1 FROM app_meta WHERE key = 'k'")).toHaveLength(0);
    expect(driver.secretSets).toBe(2);
  });

  it("keeps the passphrase while another user's database exists", async () => {
    await db.open(ALICE);
    await db.open(BOB);
    await db.deleteUserDatabase(ALICE);
    expect(driver.secret).toBeDefined();
    expect(await driver.exists(await nameOf(BOB))).toBe(true);
    expect(await db.query("SELECT 1")).toEqual([{ 1: 1 }]); // Bob is still open and working
  });

  it("keeps the passphrase when the listing looks odd rather than risk an unreadable database", async () => {
    await db.open(ALICE);
    driver.listOverride = ["some-unexpected-name"];
    await db.deleteUserDatabase(ALICE);
    expect(driver.secret).toBeDefined();
    expect(driver.calls).not.toContain("clearSecret");
  });

  it("wipes a user marked at logout the next time someone opens, even after a crash", async () => {
    await db.open(ALICE);
    await put("k", "v");
    await db.close();
    await db.markLogout(ALICE); // the app died before the delete

    const afterRestart = new NativeDatabase(driver, marks);
    await afterRestart.open(BOB);
    expect(await driver.exists(await nameOf(ALICE))).toBe(false);
    expect(marks.hashes.size).toBe(0);
    expect(await driver.exists(await nameOf(BOB))).toBe(true);
  });

  it("keeps the mark when the wipe fails, and still opens the new user's database", async () => {
    await db.open(ALICE);
    await db.close();
    await db.markLogout(ALICE);
    const failing = Object.create(driver) as MemoryNativeDriver;
    failing.delete = () => Promise.reject(new Error("disk busy"));

    await new NativeDatabase(failing, marks).open(BOB);
    expect(marks.hashes.size).toBe(1);
  });

  it("writes the header before asking whether the file is encrypted", async () => {
    // On a phone a new database is an empty file, which the plugin reports as plain.
    const order: string[] = [];
    const isEncrypted = driver.isEncrypted.bind(driver);
    const script = driver.script.bind(driver);
    driver.isEncrypted = (name) => {
      order.push("isEncrypted");
      return isEncrypted(name);
    };
    driver.script = (sql) => {
      order.push(sql);
      return script(sql);
    };
    await db.open("00000000-0000-7000-8000-000000000001");
    expect(order.slice(0, 2)).toEqual(["PRAGMA user_version = 1", "isEncrypted"]);
  });
});
