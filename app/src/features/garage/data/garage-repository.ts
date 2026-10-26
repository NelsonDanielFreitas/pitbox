import type {
  Car,
  CarInput,
  CarSummary,
  Engine,
  EngineInput,
  EngineMount,
  EngineWithMount,
  Part,
  PartCreateInput,
  PartInput,
  PartWithStock,
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

  listParts(teamId: string): Promise<PartWithStock[]>;
  getPart(teamId: string, id: string): Promise<PartWithStock | undefined>;
  createPart(teamId: string, input: PartCreateInput): Promise<PartWithStock>;
  updatePart(
    teamId: string,
    id: string,
    localRev: number,
    input: PartInput,
  ): Promise<PartWithStock>;
}

export type { CarSummary, Part, Engine };
