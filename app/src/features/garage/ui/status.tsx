import { useTranslation } from "react-i18next";

import { Badge, type BadgeTone } from "@/shared/ui/Badge";

import type { AssetStatus, TyreStatus } from "../domain/models";

const assetTones: Record<AssetStatus, BadgeTone> = {
  active: "ok",
  spare: "info",
  retired: "neutral",
};

const tyreTones: Record<TyreStatus, BadgeTone> = {
  new: "info",
  used: "warn",
  worn: "crit",
  retired: "neutral",
};

export function AssetStatusBadge({ status }: { status: AssetStatus }) {
  const { t } = useTranslation();
  return <Badge tone={assetTones[status]}>{t(`garage.statuses.${status}`)}</Badge>;
}

export function TyreStatusBadge({ status }: { status: TyreStatus }) {
  const { t } = useTranslation();
  return <Badge tone={tyreTones[status]}>{t(`garage.statuses.${status}`)}</Badge>;
}

export function EngineMountBadge({ mounted }: { mounted: boolean }) {
  const { t } = useTranslation();
  return (
    <Badge tone={mounted ? "ok" : "info"}>
      {t(mounted ? "garage.statuses.mounted" : "garage.statuses.available")}
    </Badge>
  );
}
