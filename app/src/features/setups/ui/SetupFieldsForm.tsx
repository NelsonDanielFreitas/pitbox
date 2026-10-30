import { useTranslation } from "react-i18next";

import { Collapsible } from "@/shared/ui/Collapsible";
import { FormField } from "@/shared/ui/FormField";
import { SelectInput } from "@/shared/ui/SelectInput";
import { TextInput } from "@/shared/ui/TextInput";

import { setupGroups, type FieldDef, type GroupDef } from "../domain/catalogue";
import { gearRatioOf, formatGearRatio } from "../domain/transmission";
import type { FieldTexts, FormResult } from "../domain/form";
import type { SetupData } from "../domain/models";
import { getPath } from "../domain/data";
import { useSetupLabels, type SetupLabels } from "./labels";

export interface Choice {
  id: string;
  label: string;
}

export interface FieldChoices {
  /** Options for `uuid` fields, by field path (engine, tyre set). */
  [path: string]: readonly Choice[] | undefined;
}

const fieldId = (path: string) => `setup-${path.replaceAll(".", "-").replaceAll("_", "-")}`;

/** Every group of the sheet as a collapsible block of inputs. */
export function SetupFieldsForm({
  base,
  texts,
  initial,
  result,
  counts,
  showAllErrors,
  choices,
  onChange,
}: {
  base: SetupData;
  texts: FieldTexts;
  initial: FieldTexts;
  result: FormResult;
  counts: Record<string, number>;
  showAllErrors: boolean;
  choices: FieldChoices;
  onChange: (path: string, text: string) => void;
}) {
  const { t } = useTranslation();
  const labels = useSetupLabels();
  return (
    <div className="space-y-3">
      {setupGroups.map((group) => {
        const count = counts[group.id] ?? 0;
        const hasError = group.fields.some((f) => result.issues[f.path] && (showAllErrors || texts[f.path] !== initial[f.path]));
        return (
          <Collapsible
            key={group.id}
            title={labels.group(group.id)}
            defaultOpen={showAllErrors && hasError}
            summary={
              count > 0 ? (
                <span className="text-primary">{t("setups.edit.changed", { count })}</span>
              ) : null
            }
          >
            <GroupFields
              group={group}
              base={base}
              texts={texts}
              initial={initial}
              result={result}
              showAllErrors={showAllErrors}
              choices={choices}
              labels={labels}
              onChange={onChange}
            />
          </Collapsible>
        );
      })}
    </div>
  );
}

function GroupFields({
  group,
  base,
  texts,
  initial,
  result,
  showAllErrors,
  choices,
  labels,
  onChange,
}: {
  group: GroupDef;
  base: SetupData;
  texts: FieldTexts;
  initial: FieldTexts;
  result: FormResult;
  showAllErrors: boolean;
  choices: FieldChoices;
  labels: SetupLabels;
  onChange: (path: string, text: string) => void;
}) {
  const { t, i18n } = useTranslation();
  const ratio = group.id === "transmission" ? gearRatioOf(result.data) : null;
  return (
    <div className="grid gap-4 sm:grid-cols-2">
      {group.fields.map((field) => {
        const text = texts[field.path] ?? "";
        const changed = text !== (initial[field.path] ?? "");
        const issue = result.issues[field.path];
        const error = issue && (changed || showAllErrors) ? labels.rule(field, issue) : undefined;
        const before = getPath(base, field.path);
        const previous =
          changed && before !== undefined ? t("setups.edit.previous", { value: labels.value(field, before) }) : undefined;
        return (
          <FieldInput
            key={field.path}
            field={field}
            text={text}
            label={labels.field(field.path)}
            unit={labels.unit(field)}
            error={error}
            hint={previous}
            choices={choices[field.path]}
            onChange={(next) => onChange(field.path, next)}
          />
        );
      })}
      {group.id === "transmission" ? (
        <p className="text-sm font-semibold text-fg sm:col-span-2" aria-live="polite">
          {t("setups.edit.gearRatioLive", { value: formatGearRatio(ratio, i18n.language) })}
        </p>
      ) : null}
    </div>
  );
}

function FieldInput({
  field,
  text,
  label,
  unit,
  error,
  hint,
  choices,
  onChange,
}: {
  field: FieldDef;
  text: string;
  label: string;
  unit: string | undefined;
  error: string | undefined;
  hint: string | undefined;
  choices: readonly Choice[] | undefined;
  onChange: (text: string) => void;
}) {
  const { t } = useTranslation();
  const id = fieldId(field.path);
  const describedBy = error ? `${id}-error` : hint ? `${id}-hint` : undefined;

  if (field.kind === "bool") {
    return (
      <div className="space-y-1.5">
        <label htmlFor={id} className="flex min-h-12 items-center gap-3 text-sm font-semibold text-fg">
          <input
            id={id}
            type="checkbox"
            checked={text === "true"}
            aria-describedby={describedBy}
            onChange={(event) => onChange(event.target.checked ? "true" : "")}
            className="size-6 accent-primary"
          />
          {label}
        </label>
        {hint ? <p id={`${id}-hint`} className="text-xs text-muted">{hint}</p> : null}
      </div>
    );
  }

  if (field.kind === "uuid") {
    const known = choices?.some((choice) => choice.id === text) ?? false;
    return (
      <FormField id={id} label={label} error={error} hint={hint}>
        <SelectInput
          id={id}
          value={text}
          invalid={Boolean(error)}
          aria-describedby={describedBy}
          onChange={(event) => onChange(event.target.value)}
        >
          <option value="">
            {field.path.startsWith("engine") ? t("setups.edit.noEngine") : t("setups.edit.noTyres")}
          </option>
          {text !== "" && !known ? <option value={text}>{`…${text.slice(-6)}`}</option> : null}
          {(choices ?? []).map((choice) => (
            <option key={choice.id} value={choice.id}>
              {choice.label}
            </option>
          ))}
        </SelectInput>
      </FormField>
    );
  }

  const numeric = field.kind === "int" || field.kind === "number";
  // The iOS decimal pad has no minus sign, so fields that can be negative use the full keyboard.
  const canBeNegative = (field.min ?? 0) < 0;
  const inputMode = !numeric ? "text" : canBeNegative ? "text" : field.kind === "int" ? "numeric" : "decimal";
  return (
    <FormField id={id} label={label} unit={unit} error={error} hint={hint}>
      <TextInput
        id={id}
        value={text}
        inputMode={inputMode}
        autoComplete="off"
        autoCapitalize="off"
        spellCheck={false}
        invalid={Boolean(error)}
        aria-describedby={describedBy}
        onChange={(event) => onChange(event.target.value)}
      />
    </FormField>
  );
}

