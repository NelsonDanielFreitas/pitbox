import { useTranslation } from "react-i18next";

import { Alert } from "./Alert";
import { Button } from "./Button";

export function ErrorState({
  title,
  body,
  onRetry,
  onClear,
}: {
  title: string;
  body: string;
  onRetry?: () => void;
  onClear?: () => void;
}) {
  const { t } = useTranslation();
  return (
    <Alert tone="crit" title={title}>
      <p>{body}</p>
      <div className="mt-4 flex flex-wrap gap-3">
        {onRetry ? (
          <Button variant="secondary" onClick={onRetry}>
            {t("teams.retry")}
          </Button>
        ) : null}
        {onClear ? (
          <Button variant="danger" onClick={onClear}>
            {t("local.clear")}
          </Button>
        ) : null}
      </div>
    </Alert>
  );
}
