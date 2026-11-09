import { describe, expect, it } from "vitest";

import type { LogoutMarks } from "@/db/logout-marks";
import { NativeDatabase } from "@/db/native-database";
import { hashUserId } from "@/db/user-hash";
import { FakeKeystorePlugin } from "@/test/fake-keystore-plugin";
import { MemoryNativeDriver } from "@/test/native-driver";

import { CANARY, DIAGNOSTIC_USER_ID, leaveTestDatabase, runDiagnostics, type CheckResult } from "./checks";

const noMarks: LogoutMarks = {
  list: () => Promise.resolve([]),
  add: () => Promise.resolve(),
  remove: () => Promise.resolve(),
};

async function run(plugin: FakeKeystorePlugin): Promise<CheckResult[]> {
  const results: CheckResult[] = [];
  const database = new NativeDatabase(await MemoryNativeDriver.create(), noMarks);
  await runDiagnostics({ plugin, database, hashUserId }, (r) => results.push(r));
  return results;
}

describe("runDiagnostics", () => {
  it("passes every step with a healthy Keystore and database, and cleans up", async () => {
    const plugin = new FakeKeystorePlugin();
    const results = await run(plugin);
    expect(results.map((r) => r.status)).toEqual(Array<string>(results.length).fill("pass"));
    expect(results).toHaveLength(7);
    expect(plugin.hasKey(await hashUserId("00000000-0000-7000-8000-00000000d1a6"))).toBe(false);
  });

  it("warns, and does not fail, when the key is not in secure hardware", async () => {
    const plugin = new FakeKeystorePlugin();
    plugin.securityLevel = "software";
    const results = await run(plugin);
    expect(results[0]?.status).toBe("warn");
    expect(results.some((r) => r.status === "fail")).toBe(false);
  });

  it("reports a Keystore failure and skips the signing step, but still tries the cleanup", async () => {
    const plugin = new FakeKeystorePlugin();
    plugin.failWith = new Error("boom");
    const results = await run(plugin);
    expect(results[0]?.status).toBe("fail");
    expect(results.map((r) => r.name)).not.toContain("Keystore: assinar e verificar");
    expect(results.map((r) => r.name)).toContain("Keystore: apagar chave de teste");
  });

  it("leaves a closed test database holding the canary, and the next run cleans it up", async () => {
    const database = new NativeDatabase(await MemoryNativeDriver.create(), noMarks);
    expect((await leaveTestDatabase(database)).status).toBe("pass");
    expect(await database.exists(DIAGNOSTIC_USER_ID)).toBe(true);
    await database.open(DIAGNOSTIC_USER_ID);
    expect(await database.query("SELECT v FROM diag_canary")).toEqual([{ v: CANARY }]);
    await database.deleteUserDatabase(DIAGNOSTIC_USER_ID);
    expect(await database.exists(DIAGNOSTIC_USER_ID)).toBe(false);
  });
});
