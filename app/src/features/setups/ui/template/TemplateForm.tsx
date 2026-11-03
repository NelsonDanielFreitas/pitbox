import { memo, useCallback, useId, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import { Collapsible } from "@/shared/ui/Collapsible";

import type { Template, TemplateField, TemplateGroup } from "../../domain/template";
import { templateFields } from "../../domain/template";
import { valueToText, type TemplateTexts } from "../../domain/template-form";
import { TemplateFieldView } from "./TemplateField";
import { localized } from "./text";

/** A sheet with more fields than this opens with its first section only; short ones open whole. */
const OPEN_ALL_UP_TO = 60;
/** From this many sections a row of shortcuts helps to move around. */
const NAV_FROM_GROUPS = 4;

interface CellProps {
  field: TemplateField;
  text: string;
  previous: unknown;
  issue: string | undefined;
  onChange: (key: string, text: string) => void;
  compact?: boolean;
}

/**
 * One field, memoised: typing in one field must not redraw the other 150. `onChange` has to be
 * stable for that (the editor passes a `useCallback`).
 */
const FieldCell = memo(function FieldCell({ field, text, previous, issue, onChange, compact }: CellProps) {
  const handle = useCallback((next: string) => onChange(field.key, next), [onChange, field.key]);
  return <TemplateFieldView field={field} text={text} previous={previous} issue={issue} onChange={handle} compact={compact === true} />;
});

/**
 * A sheet rendered from a template: the groups and the order of the manufacturer's sheet, one
 * input per field. `base` is the version being edited (for the "before" hint and the counters).
 * Long sheets open one section at a time, with shortcuts and "open all".
 */
export function TemplateForm({
  template,
  texts,
  base,
  issues,
  onChange,
}: {
  template: Template;
  texts: TemplateTexts;
  base: Record<string, unknown>;
  issues: Record<string, string>;
  onChange: (key: string, text: string) => void;
}) {
  const { t, i18n } = useTranslation();
  const formId = useId();
  const total = useMemo(() => templateFields(template).length, [template]);
  const [opened, setOpened] = useState<ReadonlySet<string>>(
    () => new Set(total <= OPEN_ALL_UP_TO ? template.groups.map((g) => g.key) : template.groups.slice(0, 1).map((g) => g.key)),
  );
  // A section with a problem stays open: the message has to be seen.
  const withIssue = useMemo(
    () => new Set(template.groups.filter((g) => g.fields.some((f) => issues[f.key] !== undefined)).map((g) => g.key)),
    [template, issues],
  );
  const isOpen = (key: string): boolean => opened.has(key) || withIssue.has(key);
  const anchor = (key: string): string => `${formId}-${key}`;

  const setOne = (key: string, open: boolean) =>
    setOpened((previous) => {
      const next = new Set(previous);
      if (open) next.add(key);
      else next.delete(key);
      return next;
    });
  const allOpen = template.groups.every((g) => isOpen(g.key));
  const jump = (key: string) => {
    setOne(key, true);
    // Wait for the section to render, then bring it to the top of the screen.
    requestAnimationFrame(() => document.getElementById(anchor(key))?.scrollIntoView?.({ block: "start" }));
  };

  return (
    <div className="space-y-3">
      {template.groups.length >= NAV_FROM_GROUPS ? (
        <nav aria-label={t("setups.template.sectionNav")} className="-mx-1 flex items-center gap-2 overflow-x-auto px-1 pb-1">
          {template.groups.map((group) => (
            <button
              key={group.key}
              type="button"
              onClick={() => jump(group.key)}
              className="min-h-11 shrink-0 rounded-full border border-line bg-surface px-4 text-sm font-semibold text-fg focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
            >
              {localized(group.label, i18n.language)}
            </button>
          ))}
          <button
            type="button"
            onClick={() => setOpened(allOpen ? new Set() : new Set(template.groups.map((g) => g.key)))}
            className="min-h-11 shrink-0 rounded-lg px-3 text-sm font-semibold text-primary underline focus-visible:outline-2 focus-visible:outline-primary"
          >
            {allOpen ? t("setups.template.collapseAll") : t("setups.template.expandAll")}
          </button>
        </nav>
      ) : null}
      {template.groups.map((group) => {
        const filled = group.fields.filter((f) => (texts[f.key] ?? "") !== "").length;
        const changed = group.fields.filter((f) => (texts[f.key] ?? "") !== valueToText(base[f.key])).length;
        return (
          <Collapsible
            key={group.key}
            anchorId={anchor(group.key)}
            title={localized(group.label, i18n.language)}
            open={isOpen(group.key)}
            onOpenChange={(open) => setOne(group.key, open)}
            summary={
              <span className="flex flex-col items-end leading-tight sm:flex-row sm:gap-3">
                <span>{t("setups.template.filledOf", { filled, total: group.fields.length })}</span>
                {changed > 0 ? <span className="text-primary">{t("setups.edit.changed", { count: changed })}</span> : null}
              </span>
            }
          >
            {group.help ? <p className="mb-3 text-sm text-muted">{localized(group.help, i18n.language)}</p> : null}
            {group.table ? (
              <GroupTableView group={group} table={group.table} texts={texts} base={base} issues={issues} onChange={onChange} />
            ) : (
              <div className="grid gap-5 sm:grid-cols-2">
                {group.fields.map((field) => (
                  <FieldCell key={field.key} field={field} text={texts[field.key] ?? ""} previous={base[field.key]} issue={issues[field.key]} onChange={onChange} />
                ))}
              </div>
            )}
          </Collapsible>
        );
      })}
    </div>
  );
}

/**
 * A group laid out like the paper sheet (front/rear, front/centre/rear): one row per line of the
 * sheet, one column per position. Every row names itself, and each cell shows its column above
 * the input, so it still reads on a narrow screen.
 */
function GroupTableView({
  group,
  table,
  texts,
  base,
  issues,
  onChange,
}: {
  group: TemplateGroup;
  table: NonNullable<TemplateGroup["table"]>;
  texts: TemplateTexts;
  base: Record<string, unknown>;
  issues: Record<string, string>;
  onChange: (key: string, text: string) => void;
}) {
  const { i18n } = useTranslation();
  const byKey = useMemo(() => new Map(group.fields.map((f) => [f.key, f])), [group]);
  return (
    <div className="space-y-5">
      {table.rows.map((row, index) => (
        <div key={`${index}-${row.label.pt}`} role="group" aria-label={localized(row.label, i18n.language)} className="space-y-2">
          <p className="text-sm font-semibold text-fg">{localized(row.label, i18n.language)}</p>
          <div className="grid gap-3" style={{ gridTemplateColumns: `repeat(${table.columns.length}, minmax(0, 1fr))` }}>
            {row.cells.map((key, column) => {
              const field = key === "" ? undefined : byKey.get(key);
              const columnLabel = table.columns[column];
              return (
                <div key={`${index}-${column}`} className="min-w-0 space-y-1">
                  {columnLabel ? (
                    <p aria-hidden="true" className="text-xs font-semibold uppercase tracking-wide text-muted">
                      {localized(columnLabel, i18n.language)}
                    </p>
                  ) : null}
                  {field ? (
                    <FieldCell field={field} text={texts[field.key] ?? ""} previous={base[field.key]} issue={issues[field.key]} onChange={onChange} compact />
                  ) : null}
                </div>
              );
            })}
          </div>
        </div>
      ))}
    </div>
  );
}
