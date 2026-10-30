import { queryOptions } from "@tanstack/react-query";

import type { InstallFilter, ReadScope } from "../domain/models";
import type { GarageRepository } from "./garage-repository";

export const garageKeys = {
  all: ["garage"] as const,
  cars: (teamId: string) => ["garage", teamId, "cars"] as const,
  car: (teamId: string, carId: string) => ["garage", teamId, "cars", carId] as const,
  engines: (teamId: string) => ["garage", teamId, "engines"] as const,
  engine: (teamId: string, engineId: string) =>
    ["garage", teamId, "engines", engineId] as const,
  mounts: (teamId: string, engineId: string) =>
    ["garage", teamId, "engines", engineId, "mounts"] as const,
  tyreSets: (teamId: string) => ["garage", teamId, "tyres"] as const,
  tyreSet: (teamId: string, tyreSetId: string) =>
    ["garage", teamId, "tyres", tyreSetId] as const,
  parts: (teamId: string, costs = false) => ["garage", teamId, "parts", { costs }] as const,
  part: (teamId: string, partId: string, costs = false) =>
    ["garage", teamId, "parts", partId, { costs }] as const,
  shopping: (teamId: string, costs = false) =>
    ["garage", teamId, "shopping", { costs }] as const,
  count: (teamId: string) => ["garage", teamId, "count"] as const,
  installs: (teamId: string, filter: InstallFilter, history: boolean) =>
    ["garage", teamId, "installs", filter, history] as const,
  movements: (teamId: string, partId: string, costs = false) =>
    ["garage", teamId, "parts", partId, "movements", { costs }] as const,
};

const localQuery = {
  staleTime: 5_000,
  refetchOnWindowFocus: true,
  networkMode: "always" as const,
} as const;

export function carsQueryOptions(repository: GarageRepository, teamId: string) {
  return queryOptions({
    queryKey: garageKeys.cars(teamId),
    queryFn: () => repository.listCars(teamId),
    ...localQuery,
  });
}

export function carQueryOptions(
  repository: GarageRepository,
  teamId: string,
  carId: string,
) {
  return queryOptions({
    queryKey: garageKeys.car(teamId, carId),
    queryFn: () => repository.getCar(teamId, carId),
    ...localQuery,
  });
}

export function enginesQueryOptions(repository: GarageRepository, teamId: string) {
  return queryOptions({
    queryKey: garageKeys.engines(teamId),
    queryFn: () => repository.listEngines(teamId),
    ...localQuery,
  });
}

export function engineQueryOptions(
  repository: GarageRepository,
  teamId: string,
  engineId: string,
) {
  return queryOptions({
    queryKey: garageKeys.engine(teamId, engineId),
    queryFn: () => repository.getEngine(teamId, engineId),
    ...localQuery,
  });
}

export function mountsQueryOptions(
  repository: GarageRepository,
  teamId: string,
  engineId: string,
) {
  return queryOptions({
    queryKey: garageKeys.mounts(teamId, engineId),
    queryFn: () => repository.listEngineMounts(teamId, engineId),
    ...localQuery,
  });
}

export function tyreSetsQueryOptions(repository: GarageRepository, teamId: string) {
  return queryOptions({
    queryKey: garageKeys.tyreSets(teamId),
    queryFn: () => repository.listTyreSets(teamId),
    ...localQuery,
  });
}

export function tyreSetQueryOptions(
  repository: GarageRepository,
  teamId: string,
  tyreSetId: string,
) {
  return queryOptions({
    queryKey: garageKeys.tyreSet(teamId, tyreSetId),
    queryFn: () => repository.getTyreSet(teamId, tyreSetId),
    ...localQuery,
  });
}

export function partsQueryOptions(
  repository: GarageRepository,
  teamId: string,
  scope: ReadScope = { costs: false },
) {
  return queryOptions({
    queryKey: garageKeys.parts(teamId, scope.costs),
    queryFn: () => repository.listParts(teamId, scope),
    ...localQuery,
  });
}

export function partQueryOptions(
  repository: GarageRepository,
  teamId: string,
  partId: string,
  scope: ReadScope = { costs: false },
) {
  return queryOptions({
    queryKey: garageKeys.part(teamId, partId, scope.costs),
    queryFn: () => repository.getPart(teamId, partId, scope),
    ...localQuery,
  });
}

export function stockMovementsQueryOptions(
  repository: GarageRepository,
  teamId: string,
  partId: string,
  scope: ReadScope = { costs: false },
) {
  return queryOptions({
    queryKey: garageKeys.movements(teamId, partId, scope.costs),
    queryFn: () => repository.listStockMovements(teamId, partId, scope),
    ...localQuery,
  });
}

export function installsQueryOptions(
  repository: GarageRepository,
  teamId: string,
  filter: InstallFilter,
  history = false,
) {
  return queryOptions({
    queryKey: garageKeys.installs(teamId, filter, history),
    queryFn: () => repository.listPartInstalls(teamId, filter, history),
    ...localQuery,
  });
}

export function countDraftQueryOptions(repository: GarageRepository, teamId: string) {
  return queryOptions({
    queryKey: garageKeys.count(teamId),
    queryFn: () => repository.getCountDraft(teamId),
    ...localQuery,
  });
}

export function shoppingQueryOptions(
  repository: GarageRepository,
  teamId: string,
  scope: ReadScope = { costs: false },
) {
  return queryOptions({
    queryKey: garageKeys.shopping(teamId, scope.costs),
    queryFn: () => repository.listShoppingList(teamId, scope),
    ...localQuery,
  });
}
