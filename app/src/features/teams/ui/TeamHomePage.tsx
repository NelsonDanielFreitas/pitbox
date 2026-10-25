import { useTranslation } from "react-i18next";

import { EmptyState } from "@/shared/ui/EmptyState";

import type { Team } from "../domain/team";
import { TeamTopBar } from "./TeamTopBar";

export function TeamHomePage({ team }: { team: Team }) {
  const { t } = useTranslation();
  return (
    <div className="space-y-6">
      <TeamTopBar team={team} />
      <EmptyState title={t("teams.homeTitle")} body={t("teams.homeBody")} />
    </div>
  );
}
