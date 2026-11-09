import type { TFunction } from "i18next";

import type { TemplateField } from "../../domain/template";
import { valueToText } from "../../domain/template-form";

/** Portuguese is required in a template; English falls back to it. */
export function localized(text: { pt: string; en?: string | undefined }, language: string): string {
  return language.startsWith("en") && text.en ? text.en : text.pt;
}

/** Why a value was refused, in words; never includes the value. */
export function ruleText(t: TFunction, field: TemplateField, ruleId: string): string {
  switch (ruleId) {
    case "setup.data.range":
      return t("setups.template.rule.range", { min: valueToText(field.min), max: valueToText(field.max) });
    case "setup.data.step":
      return t("setups.template.rule.step", { step: valueToText(field.step) });
    case "setup.data.length":
      return t("setups.template.rule.length", { max: field.max_len ?? 0 });
    case "setup.data.option_unknown":
      return t("setups.template.rule.option");
    case "setup.data.unknown":
      return t("setups.template.rule.unknown");
    case "setup.data.type":
      return t("setups.template.rule.type");
    default:
      return t("setups.template.rule.other");
  }
}

/** The value of the field with its unit, for the kit line and the previous-value hint. */
export function shownValue(field: TemplateField, value: unknown, yes: string, no: string): string {
  if (value === undefined) return "—";
  if (typeof value === "boolean") return value ? yes : no;
  if (Array.isArray(value)) return value.map((v) => String(v)).join(", "); // multi: the ticked options
  const text = valueToText(value);
  return field.unit ? `${text} ${field.unit}` : text;
}

