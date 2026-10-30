import { queryOptions } from "@tanstack/react-query";

import type { SetupRepository } from "./setup-repository";

export const setupKeys = {
  all: ["setups"] as const,
  list: (teamId: string, carId?: string) => ["setups", teamId, "list", carId ?? null] as const,
  setup: (teamId: string, setupId: string) => ["setups", teamId, "setup", setupId] as const,
  versions: (teamId: string, setupId: string) =>
    ["setups", teamId, "setup", setupId, "versions"] as const,
  version: (teamId: string, setupId: string, version: number | "current") =>
    ["setups", teamId, "setup", setupId, "version", version] as const,
  feedback: (teamId: string, setupId: string, version?: number) =>
    ["setups", teamId, "setup", setupId, "feedback", version ?? null] as const,
};

const localQuery = {
  staleTime: 5_000,
  refetchOnWindowFocus: true,
  networkMode: "always" as const,
} as const;

export function setupsQueryOptions(repository: SetupRepository, teamId: string, carId?: string) {
  return queryOptions({
    queryKey: setupKeys.list(teamId, carId),
    queryFn: () => repository.listSetups(teamId, carId),
    ...localQuery,
  });
}

export function setupQueryOptions(repository: SetupRepository, teamId: string, setupId: string) {
  return queryOptions({
    queryKey: setupKeys.setup(teamId, setupId),
    // TanStack Query forbids `undefined` as data, so "not found" is `null`.
    queryFn: async () => (await repository.getSetup(teamId, setupId)) ?? null,
    ...localQuery,
  });
}

export function versionsQueryOptions(repository: SetupRepository, teamId: string, setupId: string) {
  return queryOptions({
    queryKey: setupKeys.versions(teamId, setupId),
    queryFn: () => repository.listVersions(teamId, setupId),
    ...localQuery,
  });
}

export function versionQueryOptions(
  repository: SetupRepository,
  teamId: string,
  setupId: string,
  version?: number,
) {
  return queryOptions({
    queryKey: setupKeys.version(teamId, setupId, version ?? "current"),
    queryFn: async () => (await repository.getVersion(teamId, setupId, version)) ?? null,
    ...localQuery,
  });
}

export function feedbackQueryOptions(
  repository: SetupRepository,
  teamId: string,
  setupId: string,
  version?: number,
) {
  return queryOptions({
    queryKey: setupKeys.feedback(teamId, setupId, version),
    queryFn: () => repository.listFeedback(teamId, setupId, version),
    ...localQuery,
  });
}
