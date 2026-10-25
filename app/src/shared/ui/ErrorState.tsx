import { useTranslation } from "react-i18next";

import { Button } from "./Button";
import { Card } from "./Card";

export function ErrorState({
  title,
  body,
  onRetry,
}: {
  title: string;
  body: string;
  onRetry?: () => void;
}) {
  const { t } = useTranslation();
  return (
    <Card role="alert" className="border-red-200 bg-red-50">
      <h2 className="text-lg font-semibold text-pitbox-ink">{title}</h2>
      <p className="mt-1 text-sm text-pitbox-muted">{body}</p>
      {onRetry ? (
        <Button className="mt-4" variant="secondary" onClick={onRetry}>
          {t("teams.retry")}
        </Button>
      ) : null}
    </Card>
  );
}
