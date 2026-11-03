import { useQueries } from "@tanstack/react-query";

import { templateQueryOptions } from "../../data/template-queries";
import { GENERIC_TEMPLATE_ID, type TemplateRepository } from "../../data/template-repository";
import type { VersionView } from "../../domain/compare";
import type { SetupVersion } from "../../domain/models";
import { templateRef, type Template } from "../../domain/template";
import { v1ToValues } from "../../domain/values";

const isRecord = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v);

export interface VersionViews {
  isPending: boolean;
  /** The templates the versions were made with (the generic one for v1). */
  templates: Template[];
  /** The view of a version; undefined while loading or when its sheet is not on this phone. */
  viewOf(version: SetupVersion): VersionView | undefined;
  /** True when a version's own sheet is missing locally (a v2 version before its Pull). */
  missing(version: SetupVersion): boolean;
}

/**
 * Reads versions through their templates: v2 through the version it names, v1 as the generic
 * template (ADR 0030). Stored rows are never rewritten; this is only how they are read.
 */
export function useVersionViews(repository: TemplateRepository, teamId: string, versions: readonly SetupVersion[]): VersionViews {
  const needed = new Map<string, { id: string; version: number | undefined }>();
  needed.set(`${GENERIC_TEMPLATE_ID}:current`, { id: GENERIC_TEMPLATE_ID, version: undefined });
  for (const v of versions) {
    if (v.schemaVersion !== 2) continue;
    const ref = templateRef(v.data);
    if (ref) needed.set(`${ref.id}:${ref.version}`, ref);
  }
  const entries = [...needed.entries()];
  const results = useQueries({
    queries: entries.map(([, ref]) => templateQueryOptions(repository, teamId, ref.id, ref.version)),
  });
  const byKey = new Map<string, Template | null>();
  let isPending = false;
  results.forEach((r, i) => {
    const key = entries[i]?.[0];
    if (key === undefined) return;
    if (r.isPending) isPending = true;
    byKey.set(key, r.data ?? null);
  });
  const generic = byKey.get(`${GENERIC_TEMPLATE_ID}:current`) ?? null;

  const viewOf = (version: SetupVersion): VersionView | undefined => {
    if (version.schemaVersion === 2) {
      const ref = templateRef(version.data);
      const template = ref ? byKey.get(`${ref.id}:${ref.version}`) : null;
      if (!template) return undefined;
      return {
        template,
        values: isRecord(version.data.values) ? version.data.values : {},
        refs: isRecord(version.data.refs) ? version.data.refs : {},
      };
    }
    if (!generic) return undefined;
    return { template: generic, ...v1ToValues(generic, version.data) };
  };
  return {
    isPending,
    templates: [...byKey.values()].filter((t): t is Template => t !== null),
    viewOf,
    missing: (version) => !isPending && viewOf(version) === undefined,
  };
}
