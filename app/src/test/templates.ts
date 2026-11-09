import type { TemplateRepository } from "@/features/setups/data/template-repository";

export function emptyTemplateRepository(): TemplateRepository {
  return {
    listTemplates: () => Promise.resolve([]),
    getTemplateInfo: () => Promise.resolve(undefined),
    getTemplate: () => Promise.resolve(undefined),
    duplicateForTeam: () => Promise.reject(new Error("not implemented")),
  };
}
