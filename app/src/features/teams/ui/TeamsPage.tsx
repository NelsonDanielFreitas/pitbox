import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorState } from "@/shared/ui/ErrorState";
import { PageHeader } from "@/shared/ui/PageHeader";
import { Spinner } from "@/shared/ui/Spinner";

import { teamsQueryOptions } from "../data/teams-query";
import type { TeamsRepository } from "../data/teams-repository";
import { TeamCard } from "./TeamCard";

export function TeamsPage({ repository }: { repository: TeamsRepository }) {
  const { t } = useTranslation();
  const teams = useQuery(teamsQueryOptions(repository));

  if (teams.isPending) {
    return (
      <div aria-busy="true" role="status" className="flex items-center gap-3 text-muted">
        <Spinner />
        <span>{t("teams.loading")}</span>
      </div>
    );
  }

  if (teams.isError) {
    return (
      <ErrorState
        title={t("teams.errorTitle")}
        body={t("teams.errorBody")}
        onRetry={() => void teams.refetch()}
      />
    );
  }

  return (
    <>
      <PageHeader title={t("teams.title")} subtitle={t("teams.subtitle")} />
      {teams.data.length === 0 ? (
        <EmptyState title={t("teams.emptyTitle")} body={t("teams.emptyBody")} />
      ) : (
        <ul className="grid gap-3">
          {teams.data.map((team) => (
            <li key={team.id}>
              <TeamCard team={team} />
            </li>
          ))}
        </ul>
      )}
    </>
  );
}
