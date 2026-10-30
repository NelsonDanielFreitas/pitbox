import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import { useSession } from "@/auth/session-query";
import { Button } from "@/shared/ui/Button";
import { Card } from "@/shared/ui/Card";

import { installsQueryOptions } from "../data/garage-queries";
import type { GarageRepository } from "../data/garage-repository";
import { formatDate } from "../domain/format";
import type { PartInstall } from "../domain/models";
import { MountSheet, type MountSubject } from "./MountSheet";
import { UnmountSheet } from "./UnmountSheet";

/**
 * Parts mounted right now (and, for a part, the history) in a part, car or engine page.
 * Usage counters stay at 0 until sessions exist (F2).
 */
export function MountsSection({
  teamId,
  repository,
  subject,
  canWrite,
}: {
  teamId: string;
  repository: GarageRepository;
  subject: MountSubject;
  canWrite: boolean;
}) {
  const { t, i18n } = useTranslation();
  const filter =
    subject.kind === "part"
      ? { partId: subject.partId }
      : subject.kind === "car"
        ? { carId: subject.carId }
        : { engineId: subject.engineId };
  const showHistory = subject.kind === "part";
  const installs = useQuery(installsQueryOptions(repository, teamId, filter, showHistory));
  const session = useSession();
  const userId = session.data?.authenticated ? session.data.user_id : null;
  const [mountOpen, setMountOpen] = useState(false);
  const [closing, setClosing] = useState<PartInstall | null>(null);

  const list = installs.data ?? [];
  const title = subject.kind === "part" ? t("garage.partMount.mountedNow") : t("garage.partMount.sectionTitle");

  return (
    <section aria-labelledby={`mounts-${subject.kind}`} className="mt-6">
      <Card>
        <div className="mb-3 flex items-center justify-between gap-3">
          <h2 id={`mounts-${subject.kind}`} className="text-base font-semibold">
            {title}
          </h2>
          {canWrite ? (
            <Button variant="secondary" onClick={() => setMountOpen(true)}>
              {t("garage.partMount.action")}
            </Button>
          ) : null}
        </div>
        {list.length === 0 ? (
          <p className="text-sm text-muted">{t("garage.partMount.empty")}</p>
        ) : (
          <ul className="grid gap-2">
            {list.map((install) => (
              <li key={install.id} className="flex items-center gap-3 rounded-xl border border-line p-3">
                <div className="min-w-0 flex-1">
                  <p className="truncate font-semibold text-fg">
                    {subject.kind === "part" ? install.targetName : install.partDescription}
                  </p>
                  <p className="text-xs text-muted">
                    {install.removedAt === null
                      ? t("garage.partMount.since", { date: formatDate(install.installedAt, i18n.language) })
                      : t("garage.partMount.period", {
                          from: formatDate(install.installedAt, i18n.language),
                          to: formatDate(install.removedAt, i18n.language),
                        })}
                    {install.removalDestination
                      ? ` · ${t(`garage.partUnmount.destinations.${install.removalDestination}`)}`
                      : ""}
                  </p>
                  <p className="text-xs text-muted">
                    {install.sessions === 0
                      ? t("garage.partMount.noUsage")
                      : t("garage.partMount.usage", {
                          sessions: install.sessions,
                          minutes: Math.round(install.runSeconds / 60),
                        })}
                  </p>
                </div>
                {canWrite && install.removedAt === null ? (
                  <Button variant="secondary" onClick={() => setClosing(install)}>
                    {t("garage.actions.unmount")}
                  </Button>
                ) : null}
              </li>
            ))}
          </ul>
        )}
      </Card>
      {canWrite ? (
        <>
          <MountSheet
            open={mountOpen}
            onClose={() => setMountOpen(false)}
            teamId={teamId}
            repository={repository}
            userId={userId}
            subject={subject}
          />
          <UnmountSheet
            install={closing}
            onClose={() => setClosing(null)}
            teamId={teamId}
            repository={repository}
            userId={userId}
          />
        </>
      ) : null}
    </section>
  );
}
