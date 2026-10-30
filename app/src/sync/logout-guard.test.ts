// @vitest-environment node

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { LocalGarageRepository } from "@/features/garage/data/local-garage-repository";
import { openTestDatabase, type NodeSqliteExecutor } from "@/test/sqlite";
import { teamBootstrapStatements } from "@/sync/team-statements";

import { assertSafeToErase, UnsyncedChangesError } from "./logout-guard";

const teamId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60";
let db: NodeSqliteExecutor;

beforeEach(async () => {
  db = await openTestDatabase();
  await db.transaction(
    teamBootstrapStatements([{ id: teamId, name: "A", category: "1/8 TT nitro", countryCode: "PT", role: "owner" }]),
  );
});
afterEach(async () => {
  await db.close();
});

const reason = async () => {
  try {
    await assertSafeToErase(db);
    return null;
  } catch (error) {
    return error instanceof UnsyncedChangesError ? error.reason : "other";
  }
};

describe("assertSafeToErase", () => {
  it("allows an empty database", async () => {
    expect(await reason()).toBeNull();
  });

  it("refuses while operations are waiting", async () => {
    await new LocalGarageRepository(db).createEngine(teamId, {
      make: "REDS", model: "R7", serialNumber: null, purchasedOn: null, status: "active", notes: null,
    });
    expect(await reason()).toBe("pending");
    await db.transaction([{ sql: "DELETE FROM sync_outbox" }]);
    expect(await reason()).toBeNull();
  });

  it("refuses while entities that do not sync yet hold data", async () => {
    // An open count draft is local by design (ADR 0023): finish or discard it before leaving.
    await db.exec("INSERT INTO inventory_count (team_id, started_at, base_rowid) VALUES ($t, 'n', 0)", {
      $t: teamId,
    });
    await db.transaction([{ sql: "DELETE FROM sync_outbox" }]);
    expect(await reason()).toBe("local_only");
  });
});
