import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useRouterState } from "@tanstack/react-router";
import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";

import { sessionQueryKey, useSession } from "@/auth/session-query";
import { Alert } from "@/shared/ui/Alert";
import { Button } from "@/shared/ui/Button";
import { Dialog } from "@/shared/ui/Dialog";
import { ErrorState } from "@/shared/ui/ErrorState";
import { PageHeader } from "@/shared/ui/PageHeader";
import { SelectInput } from "@/shared/ui/SelectInput";
import { Sheet } from "@/shared/ui/Sheet";
import { Spinner } from "@/shared/ui/Spinner";
import { useOnline } from "@/shared/lib/use-online";
import { newId } from "@/shared/lib/id";
import { NotFound } from "@/shared/ui/NotFound";

import { invitationsQueryKey, invitationsQueryOptions, membersQueryKey, membersQueryOptions } from "../data/members-query";
import { classifyRpcError } from "../data/errors";
import type { MembersRepository } from "../data/members-repository";
import type { Team } from "../domain/team";
import { canManageTeam, canViewTeamSettings, teamRoleTranslationKey } from "../domain/roles";
import { assignableTeamRoleSchema, type AssignableTeamRole, type TeamInvitation, type TeamMember } from "../domain/team-schemas";
import { InviteSheet } from "./management/InviteSheet";
import { MemberRow } from "./management/MemberRow";
import { assignableRole, assignableRoles, formatDate, roleOrder } from "./management/format";


export function TeamManagementPage({ team, repository }: { team: Team; repository: MembersRepository }) {
  const { t, i18n } = useTranslation();
  const navigate = useNavigate();
  const currentLocation = useRouterState({ select: (state) => state.location });
  const online = useOnline();
  const queryClient = useQueryClient();
  const session = useSession();
  const owner = canManageTeam(team.role);
  const canView = canViewTeamSettings(team.role);
  const members = useQuery({ ...membersQueryOptions(repository, team.id), enabled: canView && online });
  const invitations = useQuery({
    ...invitationsQueryOptions(repository, team.id),
    enabled: owner && online,
  });
  const [inviteOpen, setInviteOpen] = useState(false);
  const [roleTarget, setRoleTarget] = useState<TeamMember | null>(null);
  const [roleValue, setRoleValue] = useState<AssignableTeamRole>("pilot");
  const [removeTarget, setRemoveTarget] = useState<TeamMember | null>(null);
  const [revokeTarget, setRevokeTarget] = useState<TeamInvitation | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [working, setWorking] = useState(false);
  // `working` only updates on the next render; this blocks a second tap in the same tick.
  const inFlight = useRef(false);

  if (!canView) return <NotFound />;

  if (!online) {
    return (
      <main className="space-y-4">
        <PageHeader title={t("teams.members.title")} subtitle={team.name} />
        <Alert tone="warn">{t("teams.offline")}</Alert>
        {owner ? <Button variant="secondary" size="lg" full disabled>{t("teams.invite.createAction")}</Button> : null}
      </main>
    );
  }

  const invalidate = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: membersQueryKey(team.id) }),
      ...(owner ? [queryClient.invalidateQueries({ queryKey: invitationsQueryKey(team.id) })] : []),
    ]);
  };

  const handleUnauthenticated = async (error: unknown): Promise<boolean> => {
    if (classifyRpcError(error) !== "unauthenticated") return false;
    queryClient.removeQueries({ queryKey: sessionQueryKey });
    queryClient.setQueryData(sessionQueryKey, { authenticated: false });
    const returnTo = `${currentLocation.pathname}${currentLocation.searchStr}`;
    await navigate({ to: "/login", search: { returnTo } });
    return true;
  };

  // The sheet keeps the member as it was when opened; after a refetch the list is newer.
  const latestVersion = (target: TeamMember): number =>
    members.data?.find((member) => member.userId === target.userId)?.version ?? target.version;

  const changeRole = async () => {
    if (!roleTarget || !online || inFlight.current) return;
    inFlight.current = true;
    setWorking(true);
    setActionError(null);
    try {
      await repository.changeMemberRole({
        teamId: team.id,
        requestId: newId(),
        userId: roleTarget.userId,
        role: roleValue,
        version: latestVersion(roleTarget),
      });
      setRoleTarget(null);
      await invalidate();
    } catch (error) {
      if (await handleUnauthenticated(error)) return;
      setActionError(memberMutationError(t, error));
      if (isVersionConflict(error)) {
        setRoleTarget(null);
        await invalidate();
      }
    } finally {
      inFlight.current = false;
      setWorking(false);
    }
  };

  const removeMember = async () => {
    if (!removeTarget || !online || inFlight.current) return;
    inFlight.current = true;
    setWorking(true);
    setActionError(null);
    try {
      await repository.removeMember({
        teamId: team.id,
        requestId: newId(),
        userId: removeTarget.userId,
        version: latestVersion(removeTarget),
      });
      setRemoveTarget(null);
      await invalidate();
    } catch (error) {
      if (await handleUnauthenticated(error)) return;
      setActionError(memberMutationError(t, error));
      if (isVersionConflict(error)) {
        setRemoveTarget(null);
        await invalidate();
      }
    } finally {
      inFlight.current = false;
      setWorking(false);
    }
  };

  const revokeInvitation = async () => {
    if (!revokeTarget || !online || inFlight.current) return;
    inFlight.current = true;
    setWorking(true);
    setActionError(null);
    try {
      await repository.revokeInvitation({ teamId: team.id, requestId: newId(), invitationId: revokeTarget.id });
      setRevokeTarget(null);
      await queryClient.invalidateQueries({ queryKey: invitationsQueryKey(team.id) });
    } catch (error) {
      if (await handleUnauthenticated(error)) return;
      setActionError(memberMutationError(t, error));
    } finally {
      inFlight.current = false;
      setWorking(false);
    }
  };

  if (members.isPending || (owner && invitations.isPending)) {
    return (
      <div aria-busy="true" role="status" className="flex items-center gap-3 text-muted">
        <Spinner />
        <span>{t("teams.loadingMembers")}</span>
      </div>
    );
  }

  if (members.isError || (owner && invitations.isError)) {
    return (
      <ErrorState
        title={t("teams.members.errorTitle")}
        body={t(online ? "teams.members.errorBody" : "teams.offline")}
        onRetry={() => void Promise.all([members.refetch(), owner ? invitations.refetch() : Promise.resolve(undefined)])}
      />
    );
  }

  const sortedMembers = [...(members.data ?? [])].sort((a, b) => (roleOrder[a.role] ?? 99) - (roleOrder[b.role] ?? 99) || a.displayName.localeCompare(b.displayName, i18n.language));
  const currentUserId = session.data?.authenticated ? session.data.user_id : "";

  return (
    <main className="space-y-6">
      <PageHeader title={t("teams.members.title")} subtitle={team.name} />
      {!online ? <Alert tone="warn">{t("teams.offline")}</Alert> : null}
      {actionError ? <Alert tone="crit">{actionError}</Alert> : null}
      {owner ? (
        <Button size="lg" full disabled={!online} onClick={() => { setActionError(null); setInviteOpen(true); }}>
          {t("teams.invite.createAction")}
        </Button>
      ) : null}

      <section aria-labelledby="team-members-heading" className="space-y-3">
        <h2 id="team-members-heading" className="text-lg font-semibold text-fg">
          {t("teams.members.count", { count: sortedMembers.length })}
        </h2>
        <ul className="space-y-2">
          {sortedMembers.map((member) => (
            <MemberRow
              key={member.userId}
              member={member}
              isCurrentUser={member.userId === currentUserId}
              canManage={owner}
              onChangeRole={(target) => { setRoleTarget(target); setRoleValue(assignableRole(target.role)); setActionError(null); }}
              onRemove={(target) => { setRemoveTarget(target); setActionError(null); }}
            />
          ))}
        </ul>
      </section>

      {owner ? (
        <section aria-labelledby="team-invitations-heading" className="space-y-3">
          <h2 id="team-invitations-heading" className="text-lg font-semibold text-fg">
            {t("teams.invitations.title")}
          </h2>
          {(invitations.data ?? []).length === 0 ? (
            <p className="rounded-2xl border border-dashed border-line p-4 text-sm text-muted">{t("teams.invitations.empty")}</p>
          ) : (
            <ul className="space-y-2">
              {(invitations.data ?? []).map((invitation) => (
                <li key={invitation.id} className="flex min-h-16 items-center gap-3 rounded-2xl border border-line bg-surface p-3">
                  <span className="min-w-0 flex-1">
                    <span className="block truncate font-semibold text-fg">{invitation.email ?? t("teams.invite.linkOnly")}</span>
                    <span className="block text-xs text-muted">
                      {t(teamRoleTranslationKey(invitation.role))} · {t("teams.invitations.expires", { date: formatDate(invitation.expiresAt, i18n.language) })}
                    </span>
                  </span>
                  <Button variant="secondary" disabled={!online || working} onClick={() => { setRevokeTarget(invitation); setActionError(null); }}>
                    {t("teams.invitations.cancel")}
                  </Button>
                </li>
              ))}
            </ul>
          )}
        </section>
      ) : null}

      <InviteSheet
        open={inviteOpen}
        teamId={team.id}
        teamName={team.name}
        online={online}
        repository={repository}
        onClose={() => setInviteOpen(false)}
        onUnauthenticated={handleUnauthenticated}
      />

      <Sheet
        open={roleTarget !== null}
        title={t("teams.members.changeRoleTitle")}
        description={roleTarget?.displayName ?? ""}
        onClose={() => { setRoleTarget(null); setActionError(null); }}
      >
        {actionError ? <Alert tone="crit" className="mb-4">{actionError}</Alert> : null}
        <label htmlFor="member-role" className="block text-sm font-semibold text-fg">{t("teams.invite.role")}</label>
        <SelectInput id="member-role" value={roleValue} disabled={!online || working} onChange={(event) => {
          const parsed = assignableTeamRoleSchema.safeParse(event.currentTarget.value);
          if (parsed.success) setRoleValue(parsed.data);
        }}>
          {assignableRoles.map((role) => <option key={role} value={role}>{t(teamRoleTranslationKey(role))}</option>)}
        </SelectInput>
        <div className="mt-5 grid grid-cols-2 gap-3">
          <Button variant="secondary" size="lg" full onClick={() => setRoleTarget(null)}>{t("garage.actions.cancel")}</Button>
          <Button size="lg" full disabled={!online || working} onClick={() => void changeRole()}>{working ? t("garage.actions.saving") : t("teams.members.saveRole")}</Button>
        </div>
      </Sheet>

      <Dialog
        open={removeTarget !== null}
        title={t("teams.members.removeTitle")}
        description={t("teams.members.removeBody", { name: removeTarget?.displayName ?? "" })}
        onClose={() => setRemoveTarget(null)}
      >
        {actionError ? <Alert tone="crit" className="mb-4">{actionError}</Alert> : null}
        <div className="flex justify-end gap-3">
          <Button variant="secondary" onClick={() => setRemoveTarget(null)}>{t("garage.actions.cancel")}</Button>
          <Button variant="danger" disabled={!online || working} onClick={() => void removeMember()}>{t("teams.members.remove")}</Button>
        </div>
      </Dialog>

      <Dialog
        open={revokeTarget !== null}
        title={t("teams.invitations.revokeTitle")}
        description={t("teams.invitations.revokeBody")}
        onClose={() => setRevokeTarget(null)}
      >
        {actionError ? <Alert tone="crit" className="mb-4">{actionError}</Alert> : null}
        <div className="flex justify-end gap-3">
          <Button variant="secondary" onClick={() => setRevokeTarget(null)}>{t("garage.actions.cancel")}</Button>
          <Button variant="danger" disabled={!online || working} onClick={() => void revokeInvitation()}>{t("teams.invitations.cancel")}</Button>
        </div>
      </Dialog>
    </main>
  );
}

function isVersionConflict(error: unknown): boolean {
  const kind = classifyRpcError(error);
  return kind === "conflict" || kind === "limit";
}

function memberMutationError(t: TFunction, error: unknown): string {
  if (isVersionConflict(error)) return t("teams.errors.memberChanged");
  if (classifyRpcError(error) === "unavailable") return t("teams.errors.offline");
  return t("teams.errors.generic");
}
