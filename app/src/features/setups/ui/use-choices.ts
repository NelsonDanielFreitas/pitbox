import { useQuery } from "@tanstack/react-query";

import { enginesQueryOptions, tyreSetsQueryOptions } from "@/features/garage/data/garage-queries";
import type { GarageRepository } from "@/features/garage/data/garage-repository";

import type { FieldChoices } from "./SetupFieldsForm";

/** Engines and tyre sets of the team, as options and as a name lookup for read-only views. */
export function useGarageChoices(repository: GarageRepository, teamId: string) {
  const engines = useQuery(enginesQueryOptions(repository, teamId));
  const tyreSets = useQuery(tyreSetsQueryOptions(repository, teamId));
  const engineChoices = (engines.data ?? []).map((e) => ({ id: e.id, label: `${e.make} ${e.model}` }));
  const tyreChoices = (tyreSets.data ?? []).map((s) => ({ id: s.id, label: s.label }));
  const choices: FieldChoices = { "engine.engine_id": engineChoices, "tyres.tyre_set_id": tyreChoices };
  const names = new Map([...engineChoices, ...tyreChoices].map((c) => [c.id, c.label] as const));
  return { choices, names, isPending: engines.isPending || tyreSets.isPending };
}
