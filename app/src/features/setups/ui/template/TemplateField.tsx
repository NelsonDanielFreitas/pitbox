import { useId, useState } from "react";
import { useTranslation } from "react-i18next";

import { cn } from "@/shared/lib/cn";
import { Badge } from "@/shared/ui/Badge";
import { Sheet } from "@/shared/ui/Sheet";
import { TextInput } from "@/shared/ui/TextInput";

import type { TemplateField as Field } from "../../domain/template";
import { multiFromText, toggleMultiText, valueToText } from "../../domain/template-form";
import { normalizeMulti } from "../../domain/template";
import { FieldDiagram } from "./diagrams/FieldDiagram";
import { localized, ruleText, shownValue } from "./text";

const touch =
  "min-h-12 min-w-12 rounded-xl border px-3 text-sm font-semibold focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary";

export function TemplateFieldView({
  field,
  text,
  previous,
  issue,
  onChange,
  compact = false,
}: {
  field: Field;
  /** What the person typed or picked ("" = not set). */
  text: string;
  /** The value in the version being edited, to show what changed. */
  previous: unknown;
  issue: string | undefined;
  onChange: (text: string) => void;
  /** In a table cell: the row and the column already name the field, so the label is read, not shown. */
  compact?: boolean;
}) {
  const { t, i18n } = useTranslation();
  const id = useId();
  const [helpOpen, setHelpOpen] = useState(false);
  const label = localized(field.label, i18n.language);
  const yes = t("setups.template.flagOn");
  const no = t("setups.template.flagOff");
  const errorText = issue ? ruleText(t, field, issue) : undefined;
  // The kit of a multi field is normalised like a stored value, so it compares equal to the ticks.
  const multiKit = field.kit !== undefined && field.type === "multi" ? normalizeMulti(field, field.kit) : undefined;
  const kit = multiKit?.ok ? multiKit.value : field.kit;
  const kitText = kit !== undefined ? shownValue(field, kit, yes, no) : undefined;
  const kitAsText = kit !== undefined ? valueToText(kit) : undefined;
  const differsKit = kitAsText !== undefined && text !== "" && text !== kitAsText;
  const previousText = previous !== undefined ? valueToText(previous) : "";
  const differsPrevious = text !== previousText;
  const hasHelp = Boolean(field.help) || Boolean(field.diagram);
  const describedBy = [errorText ? `${id}-error` : null, kitText ? `${id}-kit` : null].filter(Boolean).join(" ") || undefined;

  const control = (() => {
    switch (field.type) {
      case "choice":
        return field.diagram?.kind === "hole_row" || field.diagram?.kind === "hole_grid" ? (
          <FieldDiagram field={field} value={text || undefined} label={label} onSelect={(option) => onChange(option === text ? "" : option)} />
        ) : (
          <div role="group" aria-label={label} className="flex flex-wrap gap-2">
            {(field.options ?? []).map((option) => (
              <button
                key={option}
                type="button"
                aria-pressed={option === text}
                onClick={() => onChange(option === text ? "" : option)}
                className={cn(touch, option === text ? "border-primary bg-primary/12 text-primary" : "border-line bg-surface text-fg")}
              >
                {option}
              </button>
            ))}
          </div>
        );
      case "multi": {
        const ticked = new Set(multiFromText(text));
        return (
          <div className="space-y-2">
            <div role="group" aria-label={label} className="flex flex-wrap gap-2">
              {(field.options ?? []).map((option) => {
                const on = ticked.has(option);
                return (
                  <button
                    key={option}
                    type="button"
                    aria-pressed={on}
                    onClick={() => onChange(toggleMultiText(field, text, option))}
                    className={cn(touch, "inline-flex items-center gap-1.5", on ? "border-primary bg-primary/12 text-primary" : "border-line bg-surface text-fg")}
                  >
                    {/* The tick is not only a colour: it also shows without colour vision. */}
                    <span aria-hidden="true" className={cn("w-3 text-center", !on && "invisible")}>
                      ✓
                    </span>
                    {option}
                  </button>
                );
              })}
            </div>
            <div className="flex min-h-11 items-center gap-3 text-xs text-muted" aria-live="polite">
              <span>{ticked.size > 0 ? t("setups.template.multiSelected", { count: ticked.size }) : t("setups.template.multiNone")}</span>
              {ticked.size > 0 ? (
                <button
                  type="button"
                  onClick={() => onChange("")}
                  className="min-h-11 rounded-lg px-2 font-semibold text-primary underline focus-visible:outline-2 focus-visible:outline-primary"
                >
                  {t("setups.template.multiClear")}
                </button>
              ) : null}
            </div>
          </div>
        );
      }
      case "flag":
        return (
          <div role="group" aria-label={label} className="flex gap-2">
            {[
              ["true", yes],
              ["false", no],
            ].map(([value, name]) => (
              <button
                key={value}
                type="button"
                aria-pressed={text === value}
                onClick={() => onChange(text === value ? "" : (value ?? ""))}
                className={cn(touch, "flex-1", text === value ? "border-primary bg-primary/12 text-primary" : "border-line bg-surface text-fg")}
              >
                {name}
              </button>
            ))}
          </div>
        );
      case "number":
      case "int": {
        // The iOS decimal pad has no minus sign, so fields that can be negative use the full keyboard.
        const negative = (field.min ?? 0) < 0;
        const inputMode = negative ? "text" : field.type === "int" ? "numeric" : "decimal";
        return (
          <TextInput
            id={id}
            value={text}
            inputMode={inputMode}
            autoComplete="off"
            autoCapitalize="off"
            spellCheck={false}
            invalid={Boolean(errorText)}
            aria-describedby={describedBy}
            onChange={(event) => onChange(event.target.value)}
          />
        );
      }
      case "text":
        return (
          <TextInput
            id={id}
            value={text}
            autoComplete="off"
            maxLength={field.max_len}
            invalid={Boolean(errorText)}
            aria-describedby={describedBy}
            onChange={(event) => onChange(event.target.value)}
          />
        );
    }
  })();

  const labelled = field.type === "number" || field.type === "int" || field.type === "text";
  return (
    <div className="space-y-1.5" data-field={field.key}>
      <div className="flex items-center justify-between gap-2">
        {compact ? (
          <>
            {labelled ? (
              <label htmlFor={id} className="sr-only">
                {label}
              </label>
            ) : (
              <span className="sr-only">{label}</span>
            )}
            <span className="text-xs text-muted">{field.unit ?? ""}</span>
          </>
        ) : labelled ? (
          <label htmlFor={id} className="text-sm font-semibold text-fg">
            {label}
            {field.unit ? <span className="ml-1 font-normal text-muted">({field.unit})</span> : null}
            {field.part_ref ? <span className="ml-2 text-xs font-normal text-muted">{t("setups.template.partRef", { ref: field.part_ref })}</span> : null}
          </label>
        ) : (
          <span className="text-sm font-semibold text-fg">
            {label}
            {field.part_ref ? <span className="ml-2 text-xs font-normal text-muted">{t("setups.template.partRef", { ref: field.part_ref })}</span> : null}
          </span>
        )}
        {hasHelp ? (
          <button
            type="button"
            aria-label={t("setups.template.helpOpen", { field: label })}
            onClick={() => setHelpOpen(true)}
            className="flex size-11 shrink-0 items-center justify-center rounded-full border border-line text-sm font-bold text-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
          >
            <span aria-hidden="true">?</span>
          </button>
        ) : null}
      </div>
      {control}
      {errorText ? (
        <p id={`${id}-error`} className="text-sm font-medium text-crit">
          {errorText}
        </p>
      ) : null}
      {kitText !== undefined ? (
        <div id={`${id}-kit`} className="flex flex-wrap items-center gap-2 text-xs text-muted">
          <span>{t("setups.template.kit", { value: kitText })}</span>
          {differsKit ? <Badge tone="warn">{t("setups.template.differsKit")}</Badge> : null}
          {text !== kitAsText ? (
            <button
              type="button"
              onClick={() => onChange(kitAsText ?? "")}
              className="min-h-11 rounded-lg px-2 font-semibold text-primary underline focus-visible:outline-2 focus-visible:outline-primary"
            >
              {t("setups.template.sameAsKit")}
            </button>
          ) : null}
        </div>
      ) : null}
      {differsPrevious && previous !== undefined ? (
        <p className="text-xs text-muted">{t("setups.template.previous", { value: shownValue(field, previous, yes, no) })}</p>
      ) : null}
      {helpOpen ? <HelpSheet field={field} label={label} value={text} onClose={() => setHelpOpen(false)} /> : null}
    </div>
  );
}

/** `help` is plain text: React renders it as a text node, so a tag in it shows as typed. */
function HelpSheet({ field, label, value, onClose }: { field: Field; label: string; value: string; onClose: () => void }) {
  const { t, i18n } = useTranslation();
  const help = field.help ? localized(field.help, i18n.language) : undefined;
  const typed: unknown =
    field.type === "number" || field.type === "int"
      ? text2number(value)
      : field.type === "flag"
        ? value === "" ? undefined : value === "true"
        : field.type === "multi"
          ? undefined // a multi field has no drawing
          : value || undefined;
  return (
    <Sheet open title={t("setups.template.helpTitle", { field: label })} onClose={onClose}>
      <div className="space-y-4">
        <FieldDiagram field={field} value={typed} label={label} />
        <p className="whitespace-pre-line text-sm text-fg">{help ?? t("setups.template.noHelp")}</p>
        <button
          type="button"
          onClick={onClose}
          className="min-h-12 w-full rounded-xl bg-primary px-4 font-semibold text-on-primary focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
        >
          {t("setups.template.helpClose")}
        </button>
      </div>
    </Sheet>
  );
}

function text2number(value: string): number | undefined {
  const n = Number(value.replace(",", "."));
  return value.trim() !== "" && Number.isFinite(n) ? n : undefined;
}
