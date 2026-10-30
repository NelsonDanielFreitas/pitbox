import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import { useSession } from "@/auth/session-query";
import { formatDateTime } from "@/features/garage/domain/format";
import type { TeamRole } from "@/features/teams/domain/team";
import { Button } from "@/shared/ui/Button";

import { feedbackQueryOptions } from "../data/setup-queries";
import type { SetupRepository } from "../data/setup-repository";
import { feedbackScales, type SetupFeedback } from "../domain/models";
import { canGiveFeedback } from "../domain/roles";
import { FeedbackSheet } from "./FeedbackSheet";

/** Driver feedback of one version. Every role that reads a setup can add to it. */
export function FeedbackSection({
  teamId,
  setupId,
  version,
  role,
  repository,
}: {
  teamId: string;
  setupId: string;
  version: number;
  role: TeamRole;
  repository: SetupRepository;
}) {
  const { t, i18n } = useTranslation();
  const queryClient = useQueryClient();
  const session = useSession();
  const userId = session.data?.authenticated ? session.data.user_id : null;
  const feedback = useQuery(feedbackQueryOptions(repository, teamId, setupId, version));
  const [open, setOpen] = useState(false);

  return (
    <section aria-labelledby="setup-feedback-title">
      <div className="mb-2 flex items-center justify-between gap-3">
        <h2 id="setup-feedback-title" className="text-lg font-semibold text-fg">
          {t("setups.feedback.title")}
        </h2>
        {canGiveFeedback(role) && userId ? (
          <Button variant="secondary" onClick={() => setOpen(true)}>
            {t("setups.feedback.add")}
          </Button>
        ) : null}
      </div>
      {feedback.isPending ? null : feedback.isError ? (
        <p role="alert" className="text-sm text-crit">
          {t("setups.errorBody")}
        </p>
      ) : feedback.data.length === 0 ? (
        <p className="text-sm text-muted">{t("setups.feedback.empty")}</p>
      ) : (
        <ul className="grid gap-3">
          {feedback.data.map((entry) => (
            <FeedbackItem key={entry.id} entry={entry} mine={entry.authorId === userId} locale={i18n.language} />
          ))}
        </ul>
      )}
      {open ? (
        <FeedbackSheet
          version={version}
          onClose={() => setOpen(false)}
          onSave={async (input) => {
            if (!userId) return;
            await repository.addFeedback(teamId, setupId, version, input, userId);
            await queryClient.invalidateQueries({ queryKey: ["setups", teamId] });
            setOpen(false);
          }}
        />
      ) : null}
    </section>
  );
}

function FeedbackItem({ entry, mine, locale }: { entry: SetupFeedback; mine: boolean; locale: string }) {
  const { t } = useTranslation();
  const rated = feedbackScales.flatMap((scale) => {
    const value = entry.ratings[scale];
    return value === undefined ? [] : [{ scale, value }];
  });
  return (
    <li className="rounded-2xl border border-line bg-surface p-4">
      <p className="text-xs text-muted">
        {mine ? t("setups.feedback.byYou") : t("setups.feedback.byOther")} · {formatDateTime(entry.createdAt, locale)}
        {entry.trackZone ? ` · ${entry.trackZone}` : ""}
      </p>
      {rated.length > 0 ? (
        <ul className="mt-2 flex flex-wrap gap-2">
          {rated.map(({ scale, value }) => (
            <li key={scale} className="rounded-md border border-line bg-surface-2 px-2 py-1 text-xs font-semibold text-fg">
              {t("setups.feedback.rated", { label: t(`setups.feedback.scales.${scale}`), value })}
            </li>
          ))}
        </ul>
      ) : null}
      {entry.note ? <p className="mt-2 text-sm text-fg">{entry.note}</p> : null}
    </li>
  );
}
