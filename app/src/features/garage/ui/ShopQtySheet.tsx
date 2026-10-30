import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { Button } from "@/shared/ui/Button";
import { FormActions } from "@/shared/ui/FormActions";
import { FormField, FormRootError } from "@/shared/ui/FormField";
import { SearchInput } from "@/shared/ui/SearchInput";
import { Sheet } from "@/shared/ui/Sheet";
import { TextInput } from "@/shared/ui/TextInput";
import { normalizeSearch } from "@/shared/lib/normalize-search";

import { centiToInput, formatQuantity, parseDecimalToCenti, unitLabel } from "../domain/format";
import { MAX_STOCK_CENTI, type PartWithStock } from "../domain/models";
import { suggestedQtyCenti } from "../domain/stock";

/**
 * Adds a line to the shopping list (search a part and give a quantity) or, with `part`,
 * changes the quantity of an existing line.
 */
export function ShopQtySheet({
  open,
  onClose,
  onSave,
  teamId,
  parts,
  part,
  initialQtyCenti,
  canCreate,
}: {
  open: boolean;
  onClose: () => void;
  onSave: (partId: string, qtyCenti: number) => Promise<void>;
  teamId: string;
  parts: readonly PartWithStock[];
  /** Fixed part when editing a line. */
  part?: PartWithStock;
  initialQtyCenti?: number;
  canCreate: boolean;
}) {
  const { t, i18n } = useTranslation();
  const [query, setQuery] = useState("");
  const [chosenId, setChosenId] = useState(part?.id ?? "");
  const [qty, setQty] = useState(
    part && initialQtyCenti !== undefined ? centiToInput(initialQtyCenti, part.unit) : "",
  );
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const chosen = part ?? parts.find((candidate) => candidate.id === chosenId);
  const needle = normalizeSearch(query);
  const options = parts
    .filter(
      (candidate) =>
        needle === "" ||
        normalizeSearch(
          [candidate.description, candidate.brand, candidate.reference].filter(Boolean).join(" "),
        ).includes(needle),
    )
    .slice(0, 8);

  const close = () => {
    setQuery("");
    setError(null);
    onClose();
  };

  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!chosen) return setError(t("garage.shopping.choosePart"));
    const centi = parseDecimalToCenti(qty);
    if (!/^\d+([.,]\d{1,2})?$/.test(qty.trim()) || centi <= 0 || centi > MAX_STOCK_CENTI) {
      return setError(t("garage.validation.quantity"));
    }
    if (chosen.unit === "un" && centi % 100 !== 0) return setError(t("garage.validation.wholeUnit"));
    setBusy(true);
    setError(null);
    try {
      await onSave(chosen.id, centi);
      close();
    } catch {
      setError(t("garage.conflicts.generic"));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Sheet
      open={open}
      title={part ? t("garage.shopping.editQty") : t("garage.shopping.add")}
      description={part?.description}
      onClose={close}
    >
      <form noValidate onSubmit={(event) => void submit(event)} className="space-y-4">
        <FormRootError error={error ?? undefined} />
        {part ? null : (
          <>
            <SearchInput
              label={t("garage.search")}
              placeholder={t("garage.search")}
              value={query}
              onChange={(event) => setQuery(event.target.value)}
            />
            <fieldset className="space-y-2">
              <legend className="sr-only">{t("garage.shopping.part")}</legend>
              {options.map((candidate) => (
                <label
                  key={candidate.id}
                  className="flex min-h-14 items-center gap-3 rounded-xl border border-line bg-surface px-4 text-sm"
                >
                  <input
                    type="radio"
                    name="shop-part"
                    checked={chosenId === candidate.id}
                    onChange={() => {
                      setChosenId(candidate.id);
                      if (qty === "") {
                        const suggested = suggestedQtyCenti(candidate);
                        setQty(centiToInput(Math.max(100, suggested), candidate.unit));
                      }
                    }}
                    className="size-5 accent-[var(--primary)]"
                  />
                  <span className="min-w-0">
                    <span className="block truncate font-semibold text-fg">{candidate.description}</span>
                    <span className="block truncate text-xs text-muted">
                      {[candidate.brand, candidate.reference].filter(Boolean).join(" · ")}
                      {" · "}
                      {t("garage.shopping.have", {
                        qty: formatQuantity(candidate.qtyCenti, candidate.unit, i18n.language),
                      })}
                    </span>
                  </span>
                </label>
              ))}
            </fieldset>
            {options.length === 0 && needle !== "" ? (
              canCreate ? (
                <Link
                  to="/teams/$teamId/garage/parts/new"
                  params={{ teamId }}
                  search={{ description: query.trim().slice(0, 200), then: "shopping" }}
                  className="flex min-h-14 items-center rounded-2xl border border-dashed border-primary/60 px-4 text-left font-semibold text-primary"
                >
                  {t("garage.shopping.create", { name: query.trim() })}
                </Link>
              ) : (
                <p className="text-sm text-muted">{t("garage.shopping.noMatch", { name: query.trim() })}</p>
              )
            ) : null}
          </>
        )}
        <FormField
          id="shop-qty"
          label={t("garage.shopping.qty")}
          unit={chosen ? unitLabel(chosen.unit) : undefined}
        >
          <TextInput
            id="shop-qty"
            inputMode="decimal"
            value={qty}
            onChange={(event) => setQty(event.target.value)}
          />
        </FormField>
        <FormActions>
          <Button variant="secondary" full type="button" onClick={close}>
            {t("garage.actions.cancel")}
          </Button>
          <Button type="submit" full disabled={busy}>
            {busy ? t("garage.actions.saving") : t("garage.actions.save")}
          </Button>
        </FormActions>
      </form>
    </Sheet>
  );
}
