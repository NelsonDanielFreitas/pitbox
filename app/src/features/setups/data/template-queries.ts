import { queryOptions } from "@tanstack/react-query";

import type { TemplateRepository } from "./template-repository";

export const templateKeys = {
  all: ["setups", "templates"] as const,
  list: (teamId: string) => ["setups", "templates", teamId, "list"] as const,
  version: (teamId: string, id: string, version: number | "current") =>
    ["setups", "templates", teamId, id, version] as const,
};

const localQuery = { staleTime: 5_000, refetchOnWindowFocus: true, networkMode: "always" as const } as const;

export function templatesQueryOptions(repository: TemplateRepository, teamId: string) {
  return queryOptions({
    queryKey: templateKeys.list(teamId),
    queryFn: () => repository.listTemplates(teamId),
    ...localQuery,
  });
}

export function templateQueryOptions(repository: TemplateRepository, teamId: string, id: string, version?: number) {
  return queryOptions({
    queryKey: templateKeys.version(teamId, id, version ?? "current"),
    // TanStack Query forbids `undefined` as data, so "not stored here" is `null`.
    queryFn: async () => (await repository.getTemplate(teamId, id, version)) ?? null,
    ...localQuery,
  });
}
