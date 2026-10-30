import foundation from "./0001_foundation.sql?raw";
import garageSetup from "./0002_garage_setup.sql?raw";
import alignment from "./0003_alignment.sql?raw";
import garageConstraints from "./0004_garage_constraints.sql?raw";
import stockMovementIndex from "./0005_stock_movement_index.sql?raw";
import inventory from "./0006_inventory.sql?raw";

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
];
