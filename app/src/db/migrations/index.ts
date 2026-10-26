import foundation from "./0001_foundation.sql?raw";
import garageSetup from "./0002_garage_setup.sql?raw";

export interface Migration {
  version: number;
  name: string;
  sql: string;
}

export const migrations: readonly Migration[] = [
  { version: 1, name: "foundation", sql: foundation },
  { version: 2, name: "garage_setup", sql: garageSetup },
];
