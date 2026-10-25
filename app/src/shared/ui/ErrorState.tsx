import { useTranslation } from "react-i18next";

import { Alert } from "./Alert";
import { Button } from "./Button";

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
    <Alert tone="crit" title={title}>
      <p>{body}</p>
      {onRetry ? (
        <Button className="mt-4" variant="secondary" onClick={onRetry}>
          {t("teams.retry")}
        </Button>
      ) : null}
    </Alert>
  );
}
