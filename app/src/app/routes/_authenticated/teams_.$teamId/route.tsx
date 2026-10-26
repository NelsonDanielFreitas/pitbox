import { createFileRoute, Outlet } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { z } from "zod";

import { NotFound } from "@/app/routes/__root";
import { canReadGarage } from "@/features/garage/domain/roles";
import { teamsQueryOptions } from "@/features/teams/data/teams-query";
import { TeamContext } from "@/features/teams/ui/team-context";
import { TeamTopBar } from "@/features/teams/ui/TeamTopBar";
import { ErrorState } from "@/shared/ui/ErrorState";
import { Spinner } from "@/shared/ui/Spinner";
import { TeamTabBar } from "@/shared/ui/TeamTabBar";

const teamIdSchema = z.uuid();

export const Route = createFileRoute("/_authenticated/teams_/$teamId")({
  params: {
    parse: (raw) => {
      const parsed = teamIdSchema.safeParse(raw.teamId);
      return parsed.success ? { teamId: parsed.data } : false;
    },
  },
  component: TeamLayout,
});

function TeamLayout() {
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

  return (
    <TeamContext.Provider value={team}>
      <div className="pb-24">
        <TeamTopBar team={team} />
        <div className="mt-5">
          <Outlet />
        </div>
        <TeamTabBar teamId={team.id} showGarage={canReadGarage(team.role)} />
      </div>
    </TeamContext.Provider>
  );
}
