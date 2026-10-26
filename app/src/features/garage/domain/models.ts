export type AssetStatus = "active" | "spare" | "retired";
export type TyreStatus = "new" | "used" | "worn" | "retired";

export const partKinds = [
  "glow_plug",
  "clutch_shoe",
  "clutch_spring",
  "clutch_bell",
  "bearing",
  "conrod",
  "piston_sleeve",
  "air_filter",
  "shock_oil",
  "diff_oil",
  "fuel",
  "spring",
  "tyre",
  "insert",
  "body",
  "wing",
  "other",
] as const;
export type PartKind = (typeof partKinds)[number];

export const partUnits = ["un", "ml", "l", "g"] as const;
export type PartUnit = (typeof partUnits)[number];

export const MAX_STOCK_CENTI = 9_999_999_999;

export interface Car {
  id: string;
  teamId: string;
  make: string;
  model: string;
  modelYear: number | null;
  serialNumber: string | null;
  nickname: string | null;
  transponderNumber: string | null;
  status: AssetStatus;
  scale: string;
  powertrain: string;
  notes: string | null;
  createdAt: string;
  updatedAt: string;
  version: number;
  localRev: number;
}

export interface CarInput {
  make: string;
  model: string;
  modelYear: number | null;
  serialNumber: string | null;
  nickname: string | null;
  transponderNumber: string | null;
  status: AssetStatus;
  notes: string | null;
}

export interface CarSummary {
  id: string;
  make: string;
  model: string;
  nickname: string | null;
  status: AssetStatus;
}

export interface Engine {
  id: string;
  teamId: string;
  make: string;
  model: string;
  serialNumber: string | null;
  purchasedOn: string | null;
  status: AssetStatus;
  tanksTotal: number;
  runSecondsTotal: number;
  sessionsTotal: number;
  notes: string | null;
  createdAt: string;
  updatedAt: string;
  version: number;
  localRev: number;
}

export interface EngineInput {
  make: string;
  model: string;
  serialNumber: string | null;
  purchasedOn: string | null;
  status: AssetStatus;
  notes: string | null;
}

export interface EngineWithMount extends Engine {
  currentCar: CarSummary | null;
  mountedAt: string | null;
}

export interface EngineMount {
  id: string;
  engineId: string;
  carId: string;
  car: CarSummary;
  mountedAt: string;
  unmountedAt: string | null;
}

export interface TyreSet {
  id: string;
  teamId: string;
  label: string;
  make: string;
  model: string;
  compound: string | null;
  insertType: string | null;
  gluedOn: string | null;
  status: TyreStatus;
  sessionsTotal: number;
  runSecondsTotal: number;
  wearNotes: string | null;
  createdAt: string;
  updatedAt: string;
  version: number;
  localRev: number;
}

export interface TyreSetInput {
  label: string;
  make: string;
  model: string;
  compound: string | null;
  insertType: string | null;
  gluedOn: string | null;
  status: TyreStatus;
  wearNotes: string | null;
}

export interface Part {
  id: string;
  teamId: string;
  kind: PartKind;
  brand: string | null;
  reference: string | null;
  description: string;
  unit: PartUnit;
  minStockCenti: number;
  hasServiceLife: boolean;
  createdAt: string;
  updatedAt: string;
  version: number;
  localRev: number;
}

export interface PartWithStock extends Part {
  qtyCenti: number;
  belowMin: boolean;
}

export interface PartInput {
  kind: PartKind;
  brand: string | null;
  reference: string | null;
  description: string;
  unit: PartUnit;
  minStockCenti: number;
  hasServiceLife: boolean;
}

export interface PartCreateInput extends PartInput {
  initialQtyCenti: number;
}
