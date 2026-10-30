import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { ErrorState } from "@/shared/ui/ErrorState";
import { PageHeader } from "@/shared/ui/PageHeader";
import { Spinner } from "@/shared/ui/Spinner";

export function SetupsPageHeader({
  title,
  subtitle,
  action,
}: {
  title: string;
  subtitle: string;
  action?: ReactNode;
}) {
  return (
    <div className="mb-6 flex items-start justify-between gap-3">
      <PageHeader title={title} subtitle={subtitle} />
      {action}
    </div>
  );
}

export function SetupsLoading() {
  const { t } = useTranslation();
  return (
    <div aria-busy="true" role="status" className="flex items-center gap-3 text-muted">
      <Spinner />
      <span>{t("setups.loading")}</span>
    </div>
  );
}

export function SetupsForbidden() {
  const { t } = useTranslation();
  return <ErrorState title={t("setups.noPermission")} body={t("setups.noPermissionBody")} />;
}

export function SetupsNotFound({ onRetry }: { onRetry?: () => void }) {
  const { t } = useTranslation();
  return <ErrorState title={t("setups.notFound")} body={t("setups.errorBody")} onRetry={onRetry} />;
}

export function Chevron({ className }: { className?: string }) {
  return (
    <svg aria-hidden="true" className={className ?? "size-5 shrink-0 text-muted"} viewBox="0 0 24 24" fill="none">
      <path d="m9 6 6 6-6 6" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
