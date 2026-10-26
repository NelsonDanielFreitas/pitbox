import { createFileRoute } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { z } from "zod";

import { NotFound } from "@/app/routes/__root";
import { teamsQueryOptions } from "@/features/teams/data/teams-query";
import { TeamHomePage } from "@/features/teams/ui/TeamHomePage";
import { ErrorState } from "@/shared/ui/ErrorState";
import { Spinner } from "@/shared/ui/Spinner";

const teamIdSchema = z.uuid();

export const Route = createFileRoute("/_authenticated/teams_/$teamId")({
  params: {
    parse: (raw) => {
      const parsed = teamIdSchema.safeParse(raw.teamId);
      return parsed.success ? { teamId: parsed.data } : false;
    },
  },
  component: TeamRoute,
});

function TeamRoute() {
  const { teamId } = Route.useParams();
  const { teamsRepository } = Route.useRouteContext();
  const { t } = useTranslation();
  const teams = useQuery(teamsQueryOptions(teamsRepository));

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

  const team = teams.data.find((candidate) => candidate.id === teamId);
  if (!team) return <NotFound />;
  return <TeamHomePage team={team} />;
}
