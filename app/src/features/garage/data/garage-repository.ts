import type {
  Car,
  CarInput,
  CarSummary,
  Engine,
  EngineInput,
  EngineMount,
  CountDraft,
  EngineWithMount,
  InstallFilter,
  InstallPartInput,
  Part,
  PartInstall,
  PartCreateInput,
  PartInput,
  PartKind,
  PartWithStock,
  ReadScope,
  RemovePartInput,
  ShoppingLine,
  StockChange,
  StockMovement,
  StockMovementInput,
  TyreSet,
  TyreSetInput,
} from "../domain/models";

export interface GarageRepository {
  listCars(teamId: string): Promise<Car[]>;
  getCar(teamId: string, id: string): Promise<Car | undefined>;
  createCar(teamId: string, input: CarInput): Promise<Car>;
  updateCar(teamId: string, id: string, localRev: number, input: CarInput): Promise<Car>;

  listEngines(teamId: string): Promise<EngineWithMount[]>;
  getEngine(teamId: string, id: string): Promise<EngineWithMount | undefined>;
  createEngine(teamId: string, input: EngineInput): Promise<Engine>;
  updateEngine(
    teamId: string,
    id: string,
    localRev: number,
    input: EngineInput,
  ): Promise<EngineWithMount>;
  listEngineMounts(teamId: string, engineId: string): Promise<EngineMount[]>;
  mountEngine(teamId: string, engineId: string, carId: string): Promise<void>;
  unmountEngine(teamId: string, engineId: string): Promise<void>;
  moveEngine(teamId: string, engineId: string, toCarId: string): Promise<void>;
  swapCarEngine(teamId: string, carId: string, engineId: string): Promise<void>;

  listTyreSets(teamId: string): Promise<TyreSet[]>;
  getTyreSet(teamId: string, id: string): Promise<TyreSet | undefined>;
  createTyreSet(teamId: string, input: TyreSetInput): Promise<TyreSet>;
  updateTyreSet(
    teamId: string,
    id: string,
    localRev: number,
    input: TyreSetInput,
  ): Promise<TyreSet>;

  /** Costs are only read when `scope.costs` is true (owners and mechanics). */
  listParts(teamId: string, scope?: ReadScope): Promise<PartWithStock[]>;
  getPart(teamId: string, id: string, scope?: ReadScope): Promise<PartWithStock | undefined>;
  createPart(teamId: string, input: PartCreateInput): Promise<PartWithStock>;
  updatePart(
    teamId: string,
    id: string,
    localRev: number,
    input: PartInput,
  ): Promise<PartWithStock>;
  archivePart(teamId: string, id: string, localRev: number): Promise<void>;

  listStockMovements(
    teamId: string,
    partId: string,
    scope?: ReadScope,
  ): Promise<StockMovement[]>;
  addStockMovement(teamId: string, input: StockMovementInput): Promise<StockChange>;
  /** Appends the inverse adjustment of a movement; the original stays in the history. */
  undoMovement(
    teamId: string,
    movementId: string,
    undo: { occurredOn: string; note: string; createdBy: string | null },
  ): Promise<StockChange>;

  /** Active installs, or the whole history when `history` is true. */
  listPartInstalls(teamId: string, filter: InstallFilter, history?: boolean): Promise<PartInstall[]>;
  /** Mounts one unit and writes the `installed` movement in the same transaction. */
  installPart(teamId: string, input: InstallPartInput): Promise<StockChange>;
  /** Closes an install; destination `stock` also returns the unit to the stock. */
  removePart(teamId: string, installId: string, input: RemovePartInput): Promise<void>;

  getCountDraft(teamId: string): Promise<CountDraft | null>;
  /** Opens a draft (a no-op when one is already open) and returns it. */
  startCount(teamId: string, scopeKind: PartKind | null): Promise<CountDraft>;
  /** Sets the counted quantity of a part; `null` removes the line. */
  setCountLine(teamId: string, partId: string, countedCenti: number | null): Promise<void>;
  discardCount(teamId: string): Promise<void>;
  /**
   * Turns the draft into one `adjustment` per part whose counted value differs from the
   * current stock, in one transaction, and deletes the draft. Returns how many were made.
   */
  applyCount(
    teamId: string,
    apply: { occurredOn: string; note: string; createdBy: string | null },
  ): Promise<number>;

  /** Parts below the minimum plus the lines added by hand, minus the hidden ones. */
  listShoppingList(teamId: string, scope?: ReadScope): Promise<ShoppingLine[]>;
  /** Adds a line, changes its quantity or brings a hidden one back; `null` uses the suggestion. */
  setShoppingItem(teamId: string, partId: string, qtyCenti: number | null): Promise<void>;
  /** Deletes a manual line, or hides an automatic one until the stock is back at the minimum. */
  removeShoppingItem(teamId: string, partId: string): Promise<void>;
}

export type { CarSummary, Part, Engine };