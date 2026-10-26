import type { ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { ErrorState } from "@/shared/ui/ErrorState";
import { PageHeader } from "@/shared/ui/PageHeader";

import { canWriteGarage } from "../domain/roles";
import type { TeamRole } from "@/features/teams/domain/team";

export function GaragePageHeader({
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

export function GarageEmpty({
  title,
  body,
  actionLabel,
  to,
  teamId,
}: {
  title: string;
  body: string;
  actionLabel?: string;
  to?: "cars" | "engines" | "tyres" | "parts";
  teamId: string;
}) {
  return (
    <div className="rounded-2xl border border-dashed border-line bg-surface p-6 text-center">
      <h2 className="text-lg font-semibold text-fg">{title}</h2>
      <p className="mx-auto mt-2 max-w-md text-sm text-muted">{body}</p>
      {actionLabel && to ? (
        <Link
          to={`/teams/$teamId/garage/${to}/new` as const}
          params={{ teamId }}
          className="mt-4 inline-flex min-h-12 items-center justify-center rounded-xl bg-primary px-4 text-sm font-semibold text-on-primary"
        >
          {actionLabel}
        </Link>
      ) : null}
    </div>
  );
}

export function GarageForbidden() {
  const { t } = useTranslation();
  return (
    <ErrorState
      title={t("garage.noPermission")}
      body={t("garage.noPermissionBody")}
    />
  );
}

export function WriteGuard({
  role,
  children,
}: {
  role: TeamRole;
  children: ReactNode;
}) {
  if (!canWriteGarage(role)) return <GarageForbidden />;
  return <>{children}</>;
}

export function DetailRow({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-4 border-b border-line py-2 last:border-0">
      <dt className="text-sm text-muted">{label}</dt>
      <dd className="text-right text-sm font-medium text-fg">{value || "—"}</dd>
    </div>
  );
}
