import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { DatabaseGate } from "@/app/DatabaseGate";
import { loginHref } from "@/auth/session";
import { sessionQueryKey, useSession } from "@/auth/session-query";
import { Alert } from "@/shared/ui/Alert";
import { Button } from "@/shared/ui/Button";
import { FormField } from "@/shared/ui/FormField";
import { PageHeader } from "@/shared/ui/PageHeader";
import { Spinner } from "@/shared/ui/Spinner";
import { TextInput } from "@/shared/ui/TextInput";
import { useOnline } from "@/shared/lib/use-online";
import { newId } from "@/shared/lib/id";

import { teamsQueryKey } from "../data/teams-query";
import { classifyRpcError } from "../data/errors";
import { invitationTokenFromLink, peekPendingInvitation, clearPendingInvitation, type PendingInvitation } from "../data/invitation-token";
import type { MembersRepository } from "../data/members-repository";
import type { Team } from "../domain/team";
import { teamRoleTranslationKey } from "../domain/roles";
import { RoleBadge } from "@/shared/ui/Badge";

export function InvitationPage({ repository }: { repository: MembersRepository }) {
  const { t } = useTranslation();
  const online = useOnline();
  const session = useSession();

  useEffect(() => {
    // A link is a bearer capability. Do not keep it in browser storage to bridge login.
    if (session.data?.authenticated === false) clearPendingInvitation();
  }, [session.data]);

  if (session.isPending) {
    return (
      <main className="mx-auto flex min-h-64 max-w-xl items-center justify-center gap-3 px-4 text-muted" role="status" aria-busy="true">
        <Spinner />
        <span>{t("teams.loading")}</span>
      </main>
    );
  }

  if (session.isError) {
    return (
      <main className="mx-auto max-w-xl space-y-4 px-4 py-6">
        <PageHeader title={t("teams.acceptInvite.title")} subtitle={t("teams.acceptInvite.body")} />
        <Alert tone="crit">{t("teams.errors.generic")}</Alert>
        <Button variant="secondary" onClick={() => void session.refetch()}>{t("teams.retry")}</Button>
      </main>
    );
  }

  if (!session.data.authenticated) {
    return (
      <main className="mx-auto max-w-xl space-y-5 px-4 py-6">
        <PageHeader title={t("teams.acceptInvite.signInTitle")} subtitle={t("teams.acceptInvite.signInBody")} />
        {online ? null : <Alert tone="warn">{t("teams.offline")}</Alert>}
        <Button size="lg" full disabled={!online} onClick={() => window.location.assign(loginHref("/invite"))}>
          {t("auth.cta")}
        </Button>
      </main>
    );
  }

  // The local database must be open so the accepted team can be written to it.
  return (
    <DatabaseGate userId={session.data.user_id}>
      <InvitationFlow repository={repository} />
    </DatabaseGate>
  );
}

function InvitationFlow({ repository }: { repository: MembersRepository }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const online = useOnline();
  const [pendingInvitation, setPendingInvitation] = useState<PendingInvitation | null>(() => peekPendingInvitation());
  const [linkInput, setLinkInput] = useState("");
  const [linkInputError, setLinkInputError] = useState(false);
  const [working, setWorking] = useState(false);
  const [error, setError] = useState<"invalid" | "unavailable" | null>(null);
  const [acceptedTeam, setAcceptedTeam] = useState<Team | null>(null);
  const accepting = useRef(false);
  const requestId = useRef<string | null>(null);

  useEffect(() => {
    // The in-memory copy lives in component state from here on.
    clearPendingInvitation();
  }, []);

  const accept = async (token: string) => {
    if (!online || working || accepting.current) return;
    accepting.current = true;
    setWorking(true);
    setError(null);
    requestId.current ??= newId();
    try {
      const team = await repository.acceptInvitation(token, requestId.current);
      setPendingInvitation(null);
      requestId.current = null;
      await queryClient.invalidateQueries({ queryKey: teamsQueryKey });
      setAcceptedTeam(team);
    } catch (cause) {
      const kind = classifyRpcError(cause);
      if (kind === "unauthenticated") {
        setPendingInvitation(null);
        queryClient.removeQueries({ queryKey: sessionQueryKey });
        queryClient.setQueryData(sessionQueryKey, { authenticated: false });
        accepting.current = false;
        setWorking(false);
        window.location.assign(loginHref("/invite"));
        return;
      }
      if (kind === "not_found") setError("invalid");
      else setError("unavailable");
      accepting.current = false;
      setWorking(false);
    }
  };

  const acceptPastedLink = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const token = invitationTokenFromLink(linkInput, window.location.origin);
    setLinkInput("");
    setLinkInputError(false);
    if (!token) {
      setLinkInputError(true);
      return;
    }
    setPendingInvitation({ kind: "token", token });
    setError(null);
  };

  if (acceptedTeam) {
    return (
      <main className="mx-auto max-w-xl space-y-5 px-4 py-6">
        <PageHeader title={t("teams.acceptInvite.acceptedTitle")} subtitle={acceptedTeam.name} />
        <div className="rounded-2xl border border-line bg-surface p-5">
          <p className="text-sm text-muted">{acceptedTeam.category}</p>
          <p className="mt-3 text-sm text-muted">{t("teams.acceptInvite.acceptedBody", { role: t(teamRoleTranslationKey(acceptedTeam.role)) })}</p>
          <div className="mt-4"><RoleBadge role={acceptedTeam.role} /></div>
        </div>
        <Button size="lg" full onClick={() => void navigate({ to: "/teams/$teamId", params: { teamId: acceptedTeam.id } })}>
          {t("teams.acceptInvite.openTeam")}
        </Button>
      </main>
    );
  }

  if (pendingInvitation?.kind === "invalid") {
    return <InvitationError message={t("teams.acceptInvite.invalid")} />;
  }

  if (pendingInvitation?.kind === "token") {
    return (
      <main className="mx-auto max-w-xl space-y-5 px-4 py-6">
        <PageHeader title={t("teams.acceptInvite.title")} subtitle={t("teams.acceptInvite.body")} />
        <Alert tone="info">{t("teams.acceptInvite.bearerWarning")}</Alert>
        {error ? <InvitationError message={error === "invalid" ? t("teams.acceptInvite.invalid") : t("teams.errors.offline")} inline /> : null}
        {!online ? <Alert tone="warn">{t("teams.offline")}</Alert> : null}
        <div className="grid grid-cols-2 gap-3">
          <Button variant="secondary" size="lg" full onClick={() => void navigate({ to: "/teams" })}>
            {t("teams.acceptInvite.defer")}
          </Button>
          <Button size="lg" full disabled={!online || working} onClick={() => void accept(pendingInvitation.token)}>
            {working ? t("teams.acceptInvite.accepting") : t("teams.acceptInvite.accept")}
          </Button>
        </div>
      </main>
    );
  }

  return (
    <main className="mx-auto max-w-xl space-y-5 px-4 py-6">
      <PageHeader title={t("teams.acceptInvite.pasteTitle")} subtitle={t("teams.acceptInvite.pasteBody")} />
      {!online ? <Alert tone="warn">{t("teams.offline")}</Alert> : null}
      {linkInputError ? <Alert tone="crit">{t("teams.acceptInvite.invalid")}</Alert> : null}
      {error ? <Alert tone="crit">{error === "invalid" ? t("teams.acceptInvite.invalid") : t("teams.errors.offline")}</Alert> : null}
      <form noValidate onSubmit={acceptPastedLink} className="space-y-4">
        <FormField id="invitation-link" label={t("teams.acceptInvite.pasteLabel")}>
          <TextInput
            id="invitation-link"
            type="password"
            autoComplete="off"
            autoCapitalize="none"
            spellCheck={false}
            value={linkInput}
            disabled={!online || working}
            onChange={(event) => setLinkInput(event.currentTarget.value)}
          />
        </FormField>
        <Button type="submit" size="lg" full disabled={!online || working || linkInput.trim() === ""}>
          {t("teams.acceptInvite.pasteAction")}
        </Button>
      </form>
    </main>
  );
}

function InvitationError({ message, inline = false }: { message: string; inline?: boolean }) {
  return inline ? <Alert tone="crit">{message}</Alert> : (
    <main className="mx-auto max-w-xl px-4 py-6"><Alert tone="crit">{message}</Alert></main>
  );
}
