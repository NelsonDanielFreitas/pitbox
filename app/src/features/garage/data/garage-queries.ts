import { queryOptions } from "@tanstack/react-query";

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
  parts: (teamId: string) => ["garage", teamId, "parts"] as const,
  part: (teamId: string, partId: string) => ["garage", teamId, "parts", partId] as const,
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

export function partsQueryOptions(repository: GarageRepository, teamId: string) {
  return queryOptions({
    queryKey: garageKeys.parts(teamId),
    queryFn: () => repository.listParts(teamId),
    ...localQuery,
  });
}

export function partQueryOptions(
  repository: GarageRepository,
  teamId: string,
  partId: string,
) {
  return queryOptions({
    queryKey: garageKeys.part(teamId, partId),
    queryFn: () => repository.getPart(teamId, partId),
    ...localQuery,
  });
}
