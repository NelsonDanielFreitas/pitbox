// @vitest-environment node

import { describe, expect, it } from "vitest";

import { migrations } from "@/db/migrations";
import { runMigrations } from "@/db/migrations/runner";
import { databasePragmas } from "@/db/pragmas";
import { NodeSqliteExecutor } from "@/test/sqlite";

describe("0013 sync re-pull", () => {
  it("forgets the pull cursors and nothing else", async () => {
    const db = await NodeSqliteExecutor.create();
    try {
      for (const pragma of databasePragmas) await db.exec(pragma);
      await runMigrations(db, migrations.filter((m) => m.version <= 12));
      await db.exec(
        `INSERT INTO app_meta (key, value) VALUES ('sync.cursor.a', '5'), ('sync.cursor.b', '9'),
                ('sync.catalog', 'c1'), ('sync.device.a', 'd')`,
      );
      await runMigrations(db, migrations);
      const keys = (await db.query<{ key: string }>("SELECT key FROM app_meta ORDER BY key")).map((r) => r.key);
      expect(keys).toEqual(["sync.catalog", "sync.device.a"]);
    } finally {
      await db.close();
    }
  });
});
