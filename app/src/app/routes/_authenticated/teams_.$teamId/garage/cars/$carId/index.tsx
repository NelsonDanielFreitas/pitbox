import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";

import { CarDetailPage } from "@/features/garage/ui/CarDetailPage";

import { useTeamContext } from "@/features/teams/ui/team-context";

export const Route = createFileRoute("/_authenticated/teams_/$teamId/garage/cars/$carId/")({
  params: {
    parse: (raw) => {
      const parsed = z.uuid().safeParse(raw.carId);
      return parsed.success ? { carId: parsed.data } : false;
    },
  },
  component: CarDetailRoute,
});

function CarDetailRoute() {
  const { carId } = Route.useParams();
  const team = useTeamContext();
  const { garageRepository } = Route.useRouteContext();
  return (
    <CarDetailPage
      teamId={team.id}
      carId={carId}
      role={team.role}
      repository={garageRepository}
    />
  );
}
