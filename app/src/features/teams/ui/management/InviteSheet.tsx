import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { Alert } from "@/shared/ui/Alert";
import { Button } from "@/shared/ui/Button";
import { SelectInput } from "@/shared/ui/SelectInput";
import { Sheet } from "@/shared/ui/Sheet";
import { newId } from "@/shared/lib/id";

import { invitationsQueryKey } from "../../data/members-query";
import type { MembersRepository } from "../../data/members-repository";
import { assignableRoleInfoTranslationKey, teamRoleTranslationKey } from "../../domain/roles";
import { assignableTeamRoleSchema, type AssignableTeamRole } from "../../domain/team-schemas";
import { assignableRoles } from "./format";

type Status = "idle" | "copied" | "shared" | "error";

/**
 * Creates a bearer invitation link. The link only lives in this component's state and is
 * dropped when the sheet closes; it is never rendered as text.
 */
export function InviteSheet({
  open,
  teamId,
  teamName,
  online,
  repository,
  onClose,
  onUnauthenticated,
}: {
  open: boolean;
  teamId: string;
  teamName: string;
  online: boolean;
  repository: MembersRepository;
  onClose: () => void;
  onUnauthenticated: (error: unknown) => Promise<boolean>;
}) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [role, setRole] = useState<AssignableTeamRole>("pilot");
  const [link, setLink] = useState<string | null>(null);
  const [status, setStatus] = useState<Status>("idle");
  const [working, setWorking] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const close = () => {
    setLink(null);
    setStatus("idle");
    setError(null);
    onClose();
  };

  const create = async () => {
    if (!online || working) return;
    setWorking(true);
    setError(null);
    try {
      // InviteMember is not idempotent: every attempt is a new invitation, never retried.
      const created = await repository.inviteMember({ teamId, requestId: newId(), role });
      const url = new URL("/invite", window.location.origin);
      url.hash = new URLSearchParams({ t: created.token }).toString();
      setLink(url.href);
      await queryClient.invalidateQueries({ queryKey: invitationsQueryKey(teamId) });
    } catch (cause) {
      if (await onUnauthenticated(cause)) return;
      setError(t("teams.errors.inviteCreate"));
      await queryClient.invalidateQueries({ queryKey: invitationsQueryKey(teamId) });
    } finally {
      setWorking(false);
    }
  };

  const copy = async () => {
    if (!link) return;
    setError(null);
    try {
      await navigator.clipboard.writeText(link);
      setStatus("copied");
    } catch {
      setStatus("error");
      setError(t("teams.errors.clipboard"));
    }
  };

  const share = async () => {
    if (!link || !navigator.share) {
      await copy();
      return;
    }
    setError(null);
    try {
      await navigator.share({ title: t("teams.invite.shareTitle", { team: teamName }), url: link });
      setStatus("shared");
    } catch (cause) {
      if (cause instanceof DOMException && cause.name === "AbortError") return;
      setStatus("error");
      setError(t("teams.errors.share"));
    }
  };

  return (
    <Sheet
      open={open}
      title={link ? t("teams.invite.readyTitle") : t("teams.invite.sheetTitle")}
      description={link ? t("teams.invite.readyBody", { role: t(teamRoleTranslationKey(role)) }) : t("teams.invite.sheetBody")}
      onClose={close}
    >
      {link ? (
        <div className="space-y-4">
          <Alert tone="warn">{t("teams.invite.bearerWarning")}</Alert>
          {error ? <Alert tone="crit">{error}</Alert> : null}
          {status === "copied" ? <Alert tone="ok">{t("teams.invite.copied")}</Alert> : null}
          {status === "shared" ? <Alert tone="ok">{t("teams.invite.shared")}</Alert> : null}
          <div className="grid grid-cols-2 gap-3">
            <Button size="lg" full onClick={() => void copy()}>{t("teams.invite.copy")}</Button>
            <Button variant="secondary" size="lg" full onClick={() => void share()}>{t("teams.invite.share")}</Button>
          </div>
          <Button variant="ghost" size="lg" full onClick={close}>{t("teams.invite.done")}</Button>
        </div>
      ) : (
        <div className="space-y-4">
          {error ? <Alert tone="crit">{error}</Alert> : null}
          <label htmlFor="invite-role" className="block text-sm font-semibold text-fg">{t("teams.invite.role")}</label>
          <SelectInput
            id="invite-role"
            value={role}
            disabled={working || !online}
            onChange={(event) => {
              const parsed = assignableTeamRoleSchema.safeParse(event.currentTarget.value);
              if (parsed.success) setRole(parsed.data);
            }}
          >
            {assignableRoles.map((option) => (
              <option key={option} value={option}>{t(teamRoleTranslationKey(option))}</option>
            ))}
          </SelectInput>
          <p className="text-sm text-muted">{t(assignableRoleInfoTranslationKey(role))}</p>
          <p className="text-sm text-muted">{t("teams.invite.expiry")}</p>
          <div className="grid grid-cols-2 gap-3">
            <Button variant="secondary" size="lg" full onClick={close}>{t("garage.actions.cancel")}</Button>
            <Button size="lg" full disabled={!online || working} onClick={() => void create()}>
              {working ? t("teams.invite.creating") : t("teams.invite.createLink")}
            </Button>
          </div>
        </div>
      )}
    </Sheet>
  );
}
