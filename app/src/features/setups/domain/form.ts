import { setupFields, type FieldDef } from "./catalogue";
import { compareSetupData, groupOf } from "./compare";
import { fieldToText, getPath, parseFieldText, setPath } from "./data";
import type { SetupData } from "./models";
import { ruleIds, validateSetupData, type RuleId } from "./validate";

/** What the user typed for each field, by path. Missing = nothing typed. */
export type FieldTexts = Record<string, string>;

/** The text shown in the input of a stored value (a flag is "true" or empty). */
export function textOf(field: FieldDef, data: SetupData): string {
  const value = getPath(data, field.path);
  if (field.kind === "bool") return value === true ? "true" : "";
  return fieldToText(value);
}

export function initialTexts(data: SetupData): FieldTexts {
  return Object.fromEntries(setupFields.map((field) => [field.path, textOf(field, data)]));
}

export interface FormResult {
  /** The data the texts describe: `base` with every typed field applied. */
  data: SetupData;
  /** Problems found in the form, by field path: unreadable text or a schema rule. */
  issues: Record<string, RuleId>;
}

/**
 * Applies the typed texts to `base`. Keys the form does not edit (including unknown ones)
 * stay as they were. A text that cannot be read keeps the stored value and is reported.
 */
export function applyTexts(base: SetupData, texts: FieldTexts): FormResult {
  let data = base;
  const issues: Record<string, RuleId> = {};
  for (const field of setupFields) {
    const text = texts[field.path];
    if (text === undefined) continue;
    const parsed = parseFieldText(field, text);
    if (!parsed.ok) {
      issues[field.path] = ruleIds.type;
      continue;
    }
    if (field.kind === "bool" && parsed.value === false) {
      data = setPath(data, field.path, undefined);
    } else {
      data = setPath(data, field.path, parsed.value);
    }
  }
  for (const issue of validateSetupData(data)) {
    issues[issue.path] ??= issue.ruleId;
  }
  return { data, issues };
}

/** Number of changed leaves per group id (`shocks.front`, `diffs`, ...). */
export function changesPerGroup(before: SetupData, after: SetupData): Record<string, number> {
  const counts: Record<string, number> = {};
  for (const difference of compareSetupData(before, after)) {
    const group = groupOf(difference.path);
    counts[group] = (counts[group] ?? 0) + 1;
  }
  return counts;
}

export function totalChanges(counts: Record<string, number>): number {
  return Object.values(counts).reduce((sum, n) => sum + n, 0);
}
