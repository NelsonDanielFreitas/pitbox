import foundation from "./0001_foundation.sql?raw";
import garageSetup from "./0002_garage_setup.sql?raw";
import alignment from "./0003_alignment.sql?raw";
import garageConstraints from "./0004_garage_constraints.sql?raw";
import stockMovementIndex from "./0005_stock_movement_index.sql?raw";
import inventory from "./0006_inventory.sql?raw";
import setups from "./0007_setups.sql?raw";
import sync from "./0008_sync.sql?raw";
import syncBackfill from "./0009_sync_backfill.sql?raw";
import syncInventoryBackfill from "./0010_sync_inventory_backfill.sql?raw";
import syncMountsBackfill from "./0011_sync_mounts_backfill.sql?raw";
import syncFeedbackBackfill from "./0012_sync_feedback_backfill.sql?raw";

export interface Migration {
  version: number;
  name: string;
  sql: string;
}

export const migrations: readonly Migration[] = [
  { version: 1, name: "foundation", sql: foundation },
  { version: 2, name: "garage_setup", sql: garageSetup },
  { version: 3, name: "alignment", sql: alignment },
  { version: 4, name: "garage_constraints", sql: garageConstraints },
  { version: 5, name: "stock_movement_index", sql: stockMovementIndex },
  { version: 6, name: "inventory", sql: inventory },
  { version: 7, name: "setups", sql: setups },
  { version: 8, name: "sync", sql: sync },
  { version: 9, name: "sync_backfill", sql: syncBackfill },
  { version: 10, name: "sync_inventory_backfill", sql: syncInventoryBackfill },
  { version: 11, name: "sync_mounts_backfill", sql: syncMountsBackfill },
  { version: 12, name: "sync_feedback_backfill", sql: syncFeedbackBackfill },
];
