// @vitest-environment node

import { describe, expect, it } from "vitest";

import { migrations } from "@/db/migrations";
import { runMigrations } from "@/db/migrations/runner";
import { NodeSqliteExecutor, openTestDatabase } from "@/test/sqlite";
import type { Team } from "@/features/teams/domain/team";
import { teamBootstrapStatements } from "@/sync/team-statements";

import { GarageConflictError } from "../domain/errors";
import type {
  CarInput,
  EngineInput,
  PartCreateInput,
  TyreSetInput,
} from "../domain/models";
import { LocalGarageRepository } from "./local-garage-repository";

const teamId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60";
const otherTeamId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d61";

const team: Team = {
  id: teamId,
  name: "Equipa Alpha",
  category: "1/8 TT nitro",
  countryCode: "PT",
  role: "owner",
};

const otherTeam: Team = {
  ...team,
  id: otherTeamId,
  name: "Equipa Beta",
};

function carInput(overrides: Partial<CarInput> = {}): CarInput {
  return {
    make: "Mugen",
    model: "MBX8",
    modelYear: 2024,
    serialNumber: "SER-1",
    nickname: "Carro A",
    transponderNumber: "1234567",
    status: "active",
    notes: null,
    ...overrides,
  };
}

function engineInput(overrides: Partial<EngineInput> = {}): EngineInput {
  return {
    make: "Novarossi",
    model: "Plus 21",
    serialNumber: "ENG-1",
    purchasedOn: "2025-01-15",
    status: "active",
    notes: null,
    ...overrides,
  };
}

function tyreInput(overrides: Partial<TyreSetInput> = {}): TyreSetInput {
  return {
    label: "J4",
    make: "Pro-Line",
    model: "Badlands",
    compound: "M3",
    insertType: "Closed",
    gluedOn: "2026-10-26",
    status: "new",
    wearNotes: null,
    ...overrides,
  };
}

function partInput(overrides: Partial<PartCreateInput> = {}): PartCreateInput {
  return {
    kind: "glow_plug",
    brand: "OS",
    reference: "P3",
    description: "Vela OS P3",
    unit: "un",
    specs: {},
    minStockCenti: 200,
    reorderToCenti: null,
    initialQtyCenti: 100,
    hasServiceLife: true,
    ...overrides,
  };
}

async function setup() {
  const db = await openTestDatabase();
  await db.transaction(teamBootstrapStatements([team, otherTeam]));
  let now = "2026-10-26T12:00:00.000Z";
  const repository = new LocalGarageRepository(db, () => now);
  return {
    db,
    repository,
    advance: () => {
      now = new Date(Date.parse(now) + 1_000).toISOString();
    },
  };
}

describe("LocalGarageRepository", () => {
  it("creates and updates a car without touching the server version", async () => {
    const { db, repository } = await setup();
    try {
      const car = await repository.createCar(teamId, carInput());
      expect(car.version).toBe(1);
      expect(car.localRev).toBe(0);

      const updated = await repository.updateCar(teamId, car.id, car.localRev, {
        ...carInput(),
        nickname: "Carro B",
      });
      expect(updated.nickname).toBe("Carro B");
      expect(updated.version).toBe(1);
      expect(updated.localRev).toBe(1);

      await expect(
        repository.updateCar(teamId, car.id, car.localRev, carInput()),
      ).rejects.toBeInstanceOf(GarageConflictError);
    } finally {
      await db.close();
    }
  });

  it("keeps teams isolated", async () => {
    const { db, repository } = await setup();
    try {
      const car = await repository.createCar(teamId, carInput());
      expect(await repository.listCars(teamId)).toHaveLength(1);
      expect(await repository.listCars(otherTeamId)).toEqual([]);
      expect(await repository.getCar(otherTeamId, car.id)).toBeUndefined();
    } finally {
      await db.close();
    }
  });

  it("mounts, moves, swaps and unmounts engines with history", async () => {
    const { db, repository, advance } = await setup();
    try {
      const car1 = await repository.createCar(teamId, carInput());
      const car2 = await repository.createCar(
        teamId,
        carInput({ transponderNumber: "7654321", nickname: "Carro B" }),
      );
      const engine1 = await repository.createEngine(teamId, engineInput());
      const engine2 = await repository.createEngine(
        teamId,
        engineInput({ serialNumber: "ENG-2", model: "Plus 21-2" }),
      );

      await repository.mountEngine(teamId, engine1.id, car1.id);
      let engines = await repository.listEngines(teamId);
      expect(engines.find((engine) => engine.id === engine1.id)?.currentCar?.id).toBe(car1.id);

      advance();
      await repository.moveEngine(teamId, engine1.id, car2.id);
      const mounts = await repository.listEngineMounts(teamId, engine1.id);
      expect(mounts).toHaveLength(2);
      expect(mounts[0]?.carId).toBe(car2.id);
      expect(mounts[0]?.unmountedAt).toBeNull();
      expect(mounts[1]?.unmountedAt).not.toBeNull();

      advance();
      await repository.swapCarEngine(teamId, car1.id, engine2.id);
      engines = await repository.listEngines(teamId);
      expect(engines.find((engine) => engine.id === engine2.id)?.currentCar?.id).toBe(car1.id);

      await expect(
        repository.mountEngine(teamId, engine2.id, car2.id),
      ).rejects.toBeInstanceOf(GarageConflictError);

      advance();
      await repository.unmountEngine(teamId, engine2.id);
      expect((await repository.listEngineMounts(teamId, engine2.id))[0]?.unmountedAt).not.toBeNull();
    } finally {
      await db.close();
    }
  });

  it("does not retire a mounted engine or mount a retired engine", async () => {
    const { db, repository } = await setup();
    try {
      const car = await repository.createCar(teamId, carInput());
      const engine = await repository.createEngine(teamId, engineInput());
      await repository.mountEngine(teamId, engine.id, car.id);

      await expect(
        repository.updateEngine(teamId, engine.id, engine.localRev, {
          ...engineInput(),
          status: "retired",
        }),
      ).rejects.toMatchObject({ code: "retired" });

      const retired = await repository.createEngine(
        teamId,
        engineInput({ serialNumber: "ENG-3", status: "retired" }),
      );
      await expect(
        repository.mountEngine(teamId, retired.id, car.id),
      ).rejects.toBeInstanceOf(GarageConflictError);
    } finally {
      await db.close();
    }
  });

  it("keeps tyre labels unique and part stock in centi", async () => {
    const { db, repository } = await setup();
    try {
      await repository.createTyreSet(teamId, tyreInput());
      await expect(
        repository.createTyreSet(teamId, tyreInput({ make: "Other" })),
      ).rejects.toMatchObject({ code: "tyre_label" });

      const part = await repository.createPart(teamId, partInput());
      expect(part.qtyCenti).toBe(100);
      expect(part.belowMin).toBe(true);
      expect(await repository.listParts(teamId)).toHaveLength(1);

      const updated = await repository.updatePart(teamId, part.id, part.localRev, {
        kind: "glow_plug",
        brand: "OS",
        reference: "P3",
        description: "Vela OS P3",
        unit: "un",
        specs: {},
        minStockCenti: 100,
        reorderToCenti: null,
        hasServiceLife: true,
      });
      expect(updated.qtyCenti).toBe(100);
      expect(updated.belowMin).toBe(false);
      expect(updated.localRev).toBe(1);
    } finally {
      await db.close();
    }
  });

  it("converts legacy REAL stock to centi and keeps foreign keys valid", async () => {
    const db = await NodeSqliteExecutor.create();
    try {
      await runMigrations(db, [migrations[0]!, migrations[1]!, migrations[2]!]);
      await db.transaction(teamBootstrapStatements([team]));
      await db.exec(
        `INSERT INTO part (
          id, team_id, kind, brand, reference, description, unit, specs, min_stock,
          unit_cost_cents, currency, has_service_life, created_at, updated_at, version
        ) VALUES ($id, $team, 'glow_plug', 'OS', 'P3', 'Vela', 'un', '{}', 2.5,
          NULL, 'EUR', 1, $now, $now, 1)`,
        {
          $id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d70",
          $team: teamId,
          $now: "2026-10-26T12:00:00.000Z",
        },
      );
      await db.exec(
        `INSERT INTO stock_movement (
          id, team_id, part_id, qty_delta, reason, note, occurred_at, created_at
        ) VALUES ($id, $team, $part, 1.25, 'purchase', NULL, $now, $now)`,
        {
          $id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d71",
          $team: teamId,
          $part: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d70",
          $now: "2026-10-26T12:00:00.000Z",
        },
      );

      await runMigrations(db, [migrations[3]!]);
      const part = await db.query<{ min_stock_centi: number }>(
        "SELECT min_stock_centi FROM part WHERE id = $id",
        { $id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d70" },
      );
      const movement = await db.query<{ qty_delta_centi: number }>(
        "SELECT qty_delta_centi FROM stock_movement WHERE id = $id",
        { $id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d71" },
      );
      const foreignKeys = await db.query("PRAGMA foreign_key_check");
      expect(part[0]?.min_stock_centi).toBe(250);
      expect(movement[0]?.qty_delta_centi).toBe(125);
      expect(foreignKeys).toEqual([]);
    } finally {
      await db.close();
    }
  });
});
