import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";

import { CarFormPage } from "@/features/garage/ui/CarFormPage";

import { useTeamContext } from "@/features/teams/ui/team-context";

export const Route = createFileRoute("/_authenticated/teams_/$teamId/garage/cars/$carId/edit")({
  params: {
    parse: (raw) => {
      const parsed = z.uuid().safeParse(raw.carId);
      return parsed.success ? { carId: parsed.data } : false;
    },
  },
  component: EditCarRoute,
});

function EditCarRoute() {
  const { carId } = Route.useParams();
  const team = useTeamContext();
  const { garageRepository } = Route.useRouteContext();
  return (
    <CarFormPage
      teamId={team.id}
      carId={carId}
      role={team.role}
      repository={garageRepository}
    />
  );
}
