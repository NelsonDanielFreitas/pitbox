import type { ParseKeys, TFunction } from "i18next";
import { useTranslation } from "react-i18next";

import type { FieldDef } from "../domain/catalogue";
import { fieldToText } from "../domain/data";
import { ruleIds, type RuleId } from "../domain/validate";

type FieldLabelKey = Extract<ParseKeys, `setups.fields.${string}`>;
type GroupLabelKey = Extract<ParseKeys, `setups.groups.${string}`>;

export interface SetupLabels {
  field(path: string): string;
  group(id: string): string;
  /** "500 cSt", "3,54", "Sim"; "—" when there is no value. */
  value(field: FieldDef | undefined, value: unknown): string;
  unit(field: FieldDef): string | undefined;
  /** Why a value was refused, in words; never includes the value itself. */
  rule(field: FieldDef | undefined, ruleId: RuleId): string;
}

export function useSetupLabels(): SetupLabels {
  const { t, i18n } = useTranslation();
  const has = (key: string) => i18n.exists(key);
  return makeLabels(t, has);
}

export function makeLabels(t: TFunction, has: (key: string) => boolean): SetupLabels {
  const isFieldKey = (key: string): key is FieldLabelKey => has(key);
  const isGroupKey = (key: string): key is GroupLabelKey => has(key);

  const unit = (field: FieldDef): string | undefined => {
    if (!field.unit) return undefined;
    if (field.unit === "teeth") return t("setups.units.teeth");
    if (field.unit === "turns") return t("setups.units.turns");
    return field.unit;
  };

  return {
    field(path) {
      const key = `setups.fields.${path}`;
      return isFieldKey(key) ? t(key) : path;
    },
    group(id) {
      const key = `setups.groups.${id}`;
      return isGroupKey(key) ? t(key) : id;
    },
    unit,
    value(field, value) {
      if (value === undefined || value === null) return t("setups.detail.notSet");
      if (typeof value === "boolean") return value ? t("setups.detail.yes") : t("setups.detail.no");
      const text = fieldToText(value) || JSON.stringify(value);
      const u = field ? unit(field) : undefined;
      return u ? `${text} ${u}` : text;
    },
    rule(field, ruleId) {
      switch (ruleId) {
        case ruleIds.range:
          if (field?.min !== undefined && field.max !== undefined) {
            return t("setups.validation.rule.range", { min: fieldToText(field.min), max: fieldToText(field.max) });
          }
          return t("setups.validation.rule.type");
        case ruleIds.length:
          return t("setups.validation.rule.length", { max: field?.maxLen ?? 0 });
        case ruleIds.uuid:
          return t("setups.validation.rule.uuid");
        case ruleIds.barsConflict:
          return t("setups.validation.rule.barsConflict");
        case ruleIds.unknown:
          return t("setups.validation.rule.unknown");
        case ruleIds.size:
          return t("setups.validation.rule.size");
        case ruleIds.depth:
          return t("setups.validation.rule.depth");
        case ruleIds.schemaVersion:
          return t("setups.validation.rule.schemaVersion");
        case ruleIds.type:
          return t("setups.validation.rule.type");
      }
    },
  };
}
