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

export type SpecValue = string | number;
export type PartSpecs = Record<string, SpecValue>;

export interface Part {
  id: string;
  teamId: string;
  kind: PartKind;
  brand: string | null;
  reference: string | null;
  description: string;
  unit: PartUnit;
  specs: PartSpecs;
  minStockCenti: number;
  /** "Repor até": quantity to have after buying; null means restock up to the minimum. */
  reorderToCenti: number | null;
  hasServiceLife: boolean;
  createdAt: string;
  updatedAt: string;
  version: number;
  localRev: number;
}

export interface PartWithStock extends Part {
  qtyCenti: number;
  belowMin: boolean;
  /** Unit cost in cents. `undefined` when the reader's role may not see costs. */
  unitCostCents?: number | null;
}

export interface PartInput {
  kind: PartKind;
  brand: string | null;
  reference: string | null;
  description: string;
  unit: PartUnit;
  specs: PartSpecs;
  minStockCenti: number;
  reorderToCenti: number | null;
  hasServiceLife: boolean;
  /** Only owners and mechanics may set it; `undefined` leaves the stored value alone. */
  unitCostCents?: number | null;
}

export interface PartCreateInput extends PartInput {
  initialQtyCenti: number;
  /** Id of the user creating the part (author of the initial movement). */
  createdBy?: string | null;
}

/** What a reader may see: costs are only read for owners and mechanics (ADR 0023). */
export interface ReadScope {
  costs: boolean;
}

export const stockReasons = [
  "purchase",
  "consumed",
  "installed",
  "adjustment",
  "lost",
  "returned",
] as const;
export type StockReason = (typeof stockReasons)[number];

export const lossReasons = ["broken", "worn", "lost", "expired"] as const;
export type LossReason = (typeof lossReasons)[number];

export interface StockMovement {
  id: string;
  partId: string;
  qtyDeltaCenti: number;
  reason: StockReason;
  lossReason: LossReason | null;
  note: string | null;
  /** Local-only (ADR 0023): where a purchase was made. */
  store: string | null;
  /** Price paid per unit; `undefined` when the reader's role may not see costs. */
  unitCostCents?: number | null;
  occurredAt: string;
  createdAt: string;
  /** User id of the author; the UI turns it into "you" or a member name. */
  createdBy: string | null;
  /** Id of the movement this one undoes. */
  revertsMovementId: string | null;
  /** True when another movement undoes this one. */
  undone: boolean;
}

export interface StockMovementInput {
  partId: string;
  reason: StockReason;
  /** Signed, in hundredths; the sign must match the reason (adjustments may be either). */
  qtyDeltaCenti: number;
  lossReason: LossReason | null;
  unitCostCents: number | null;
  store: string | null;
  /** ISO date (yyyy-mm-dd); the movement happened on this day. */
  occurredOn: string;
  note: string | null;
  createdBy: string | null;
}

/** Result of a stock write: lets the UI warn when the minimum was crossed. */
export interface StockChange {
  movementId: string;
  beforeCenti: number;
  afterCenti: number;
  minStockCenti: number;
}

export const removalDestinations = ["stock", "scrap", "spare"] as const;
export type RemovalDestination = (typeof removalDestinations)[number];

/** Why a scrapped unit left the car: the only reasons the server accepts for scrap. */
export const scrapReasons = ["broken", "worn", "lost"] as const;
export type ScrapReason = (typeof scrapReasons)[number];

/** One unit of a part with a service life, mounted in a car or an engine. */
export interface PartInstall {
  id: string;
  partId: string;
  partKind: PartKind;
  partDescription: string;
  partBrand: string | null;
  partReference: string | null;
  carId: string | null;
  engineId: string | null;
  /** Nickname or make and model of the car or the engine it is mounted in. */
  targetName: string;
  installedAt: string;
  removedAt: string | null;
  removalDestination: RemovalDestination | null;
  removalReason: ScrapReason | null;
  /** Stay at 0 until sessions exist (F2). */
  runSeconds: number;
  sessions: number;
  note: string | null;
}

/** Exactly one of the three, like the server's InstallFilter. */
export type InstallFilter = { partId: string } | { carId: string } | { engineId: string };

export interface InstallPartInput {
  partId: string;
  target: { carId: string } | { engineId: string };
  occurredOn: string;
  note: string | null;
  createdBy: string | null;
}

export interface RemovePartInput {
  destination: RemovalDestination;
  /** Required for scrap, forbidden otherwise. */
  reason: ScrapReason | null;
  occurredOn: string;
  note: string | null;
  createdBy: string | null;
}

/** The open inventory count of a team (local only, ADR 0023). */
export interface CountDraft {
  /** `null` counts every part. */
  scopeKind: PartKind | null;
  startedAt: string;
  /** Movements registered since the count started, for the parts already counted. */
  movementsSince: number;
  /** Counted quantity (hundredths) by part id; parts not counted yet are absent. */
  lines: Record<string, number>;
}

/** One line of the computed shopping list (parts below the minimum plus stored lines). */
export interface ShoppingLine {
  partId: string;
  kind: PartKind;
  description: string;
  brand: string | null;
  reference: string | null;
  unit: PartUnit;
  stockCenti: number;
  /** What to buy: the stored quantity, or the suggestion. */
  qtyCenti: number;
  suggestedQtyCenti: number;
  /** Below the minimum: the line is automatic; otherwise it was added by hand. */
  belowMin: boolean;
  note: string | null;
  /** `undefined` when the reader's role may not see costs. */
  unitCostCents?: number | null;
}
