import type { PitboxSecurePlugin } from "pitbox-secure";

import type { LocalDatabase } from "@/db/local-database";
import { NativeDeviceKeys } from "@/sync/device-key-native";

export type CheckStatus = "pass" | "warn" | "fail";

export interface CheckResult {
  name: string;
  status: CheckStatus;
  detail: string;
}

export type DiagnosticPlugin = Pick<
  PitboxSecurePlugin,
  "ensureDeviceKey" | "signWithDeviceKey" | "deleteDeviceKey"
>;

export interface DiagnosticDeps {
  plugin: DiagnosticPlugin;
  database: LocalDatabase;
  hashUserId: (userId: string) => Promise<string>;
}

/** A user that does not exist: the diagnostics never touch the data or the key of a real one. */
export const DIAGNOSTIC_USER_ID = "00000000-0000-7000-8000-00000000d1a6";

type Step = () => Promise<{ status?: CheckStatus; detail: string }>;

async function run(name: string, step: Step, report: (r: CheckResult) => void): Promise<boolean> {
  try {
    const { status = "pass", detail } = await step();
    report({ name, status, detail });
    return status !== "fail";
  } catch (error) {
    // Class and message only: errors from the plugins never carry keys or passphrases (ADR 0033).
    const text = error instanceof Error ? `${error.name}: ${error.message}` : "unknown error";
    report({ name, status: "fail", detail: text });
    return false;
  }
}

async function migrationCount(database: LocalDatabase): Promise<number> {
  const rows = await database.query<{ n: number }>("SELECT count(*) AS n FROM schema_migrations");
  return Number(rows[0]?.n ?? -1);
}

/**
 * Debug-only checks of what only a real phone can prove (ADR 0033): the Keystore key and the
 * encrypted database. Each step reports as soon as it ends; a failed step stops its group, and
 * the cleanup always runs. Uses a throwaway user, never a real one.
 */
export async function runDiagnostics(
  deps: DiagnosticDeps,
  report: (result: CheckResult) => void,
): Promise<void> {
  const userHash = await deps.hashUserId(DIAGNOSTIC_USER_ID);
  const keys = new NativeDeviceKeys(deps.plugin, { allowSoftwareKeys: true });

  const keyOk = await run(
    "Keystore: criar chave P-256",
    async () => {
      const { securityLevel } = await deps.plugin.ensureDeviceKey({ userHash });
      await keys.loadOrCreate(userHash); // also validates the SPKI
      const hardware = securityLevel === "strongbox" || securityLevel === "tee";
      return {
        status: hardware ? "pass" : "warn",
        detail: `nível: ${securityLevel}${hardware ? "" : " (sem hardware seguro: o sync recusaria esta chave)"}`,
      };
    },
    report,
  );

  if (keyOk) {
    await run(
      "Keystore: assinar e verificar",
      async () => {
        const signer = await keys.loadOrCreate(userHash);
        const message = new TextEncoder().encode("pitbox diagnostic");
        const signature = await signer.sign(message);
        const publicKey = await crypto.subtle.importKey(
          "spki",
          (await signer.publicKey()).slice(),
          { name: "ECDSA", namedCurve: "P-256" },
          false,
          ["verify"],
        );
        const valid = await crypto.subtle.verify(
          { name: "ECDSA", hash: "SHA-256" },
          publicKey,
          signature.slice(),
          message.slice(),
        );
        if (!valid) throw new Error("signature does not verify");
        return { detail: `assinatura de ${signature.length} bytes verificada com a chave pública` };
      },
      report,
    );
  }

  await run(
    "Keystore: apagar chave de teste",
    async () => {
      await keys.forget(userHash);
      return { detail: "removida" };
    },
    report,
  );

  const { database } = deps;
  let migrations = -1;
  let dbOk = await run(
    "Base: abrir cifrada e migrar",
    async () => {
      await database.open(DIAGNOSTIC_USER_ID);
      migrations = await migrationCount(database);
      if (migrations < 1) throw new Error("no migration applied");
      return { detail: `${migrations} migrações aplicadas` };
    },
    report,
  );

  if (dbOk) {
    dbOk = await run(
      "Base: escrever e ler",
      async () => {
        await database.exec("CREATE TEMP TABLE diag_probe (v TEXT NOT NULL)");
        await database.exec("INSERT INTO diag_probe (v) VALUES ($v)", { $v: "pitbox" });
        const rows = await database.query<{ v: string }>("SELECT v FROM diag_probe");
        if (rows.length !== 1 || rows[0]?.v !== "pitbox") throw new Error("unexpected rows");
        return { detail: "escrita e leitura com parâmetros nomeados" };
      },
      report,
    );
  }

  if (dbOk) {
    await run(
      "Base: fechar e reabrir (chave guardada)",
      async () => {
        await database.close();
        await database.open(DIAGNOSTIC_USER_ID);
        const again = await migrationCount(database);
        if (again !== migrations) throw new Error("data differs after reopening");
        return { detail: "os dados e a chave sobrevivem ao fecho" };
      },
      report,
    );
  }

  await run(
    "Base: apagar base de teste",
    async () => {
      await database.close();
      await database.deleteUserDatabase(DIAGNOSTIC_USER_ID);
      const left = await database.exists(DIAGNOSTIC_USER_ID);
      if (left) throw new Error("database still exists");
      return { detail: "removida" };
    },
    report,
  );
}

/** Text that is searched for in the copied file: if it is there, the database is not encrypted. */
export const CANARY = "PITBOX-CANARY-7f3a91c2";

/**
 * Leaves an open, populated test database on the phone for the manual copy test (runbook): the
 * file is then copied out with adb and must show neither the SQLite header nor the canary. The
 * next diagnostics run deletes it (it is the throwaway user's database).
 */
export async function leaveTestDatabase(database: LocalDatabase): Promise<CheckResult> {
  try {
    await database.open(DIAGNOSTIC_USER_ID);
    await database.exec("CREATE TABLE IF NOT EXISTS diag_canary (v TEXT NOT NULL)");
    await database.exec("INSERT INTO diag_canary (v) VALUES ($v)", { $v: CANARY });
    await database.close(); // flushed to disk, like after a normal use
    return { name: "Base de teste para o adb", status: "pass", detail: "criada e fechada" };
  } catch (error) {
    const text = error instanceof Error ? `${error.name}: ${error.message}` : "unknown error";
    return { name: "Base de teste para o adb", status: "fail", detail: text };
  }
}
