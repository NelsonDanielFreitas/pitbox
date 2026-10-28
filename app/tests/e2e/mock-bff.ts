import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import { connectNodeAdapter } from "@connectrpc/connect-node";
import { randomBytes, randomUUID } from "node:crypto";
import { createServer, type IncomingMessage, type ServerResponse } from "node:http";

import {
  InvitationSchema,
  InviteMemberResponseSchema,
  AcceptInvitationResponseSchema,
  ListInvitationsResponseSchema,
  ListMembersResponseSchema,
  MemberSchema,
  MemberService,
  RemoveMemberResponseSchema,
  RevokeInvitationResponseSchema,
} from "@/gen/pitbox/identity/v1/member_service_pb";
import {
  CreateTeamResponseSchema,
  ListMyTeamsResponseSchema,
  TeamMembershipSchema,
  TeamRole,
  TeamSchema,
  TeamService,
} from "@/gen/pitbox/identity/v1/team_service_pb";

const port = Number(process.env.PITBOX_E2E_BFF_PORT ?? 4010);
const appOrigin = process.env.PITBOX_E2E_ORIGIN ?? "http://localhost:4173";
const cookieName = "__Host-pitbox_session";
const users = {
  "mock-session": { id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60", name: "Nelson Freitas" },
  "mock-session-2": { id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d70", name: "Piloto Teste" },
} as const;
type SessionValue = keyof typeof users;
const defaultCookie: SessionValue = "mock-session";
const teamAlphaId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d61";
const teamBetaId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d62";

interface MockMember {
  teamId: string;
  userId: string;
  displayName: string;
  role: TeamRole;
  joinedAt: Date;
  version: number;
}
interface MockInvitation {
  id: string;
  teamId: string;
  role: TeamRole;
  token: string;
  createdAt: Date;
  state: "pending" | "used" | "revoked";
}

let expireRpc = false;
let offlineRpc = false;
let weakFactor = false;
let nextCookie: SessionValue = defaultCookie;
let teamRecords: ReturnType<typeof seedTeams> = seedTeams();
let members: MockMember[] = seedMembers();
let invitations: MockInvitation[] = [];

function seedTeams() {
  return [
    create(TeamSchema, { id: teamAlphaId, name: "Equipa Alpha", category: "1/8 TT nitro", countryCode: "PT" }),
    create(TeamSchema, {
      id: teamBetaId,
      name: "Equipa Beta",
      category: "1/8 TT nitro",
      club: "Clube de Braga",
      countryCode: "PT",
    }),
  ];
}

function seedMembers(): MockMember[] {
  const joinedAt = new Date("2026-10-20T12:00:00Z");
  const first = users["mock-session"];
  return [
    { teamId: teamAlphaId, userId: first.id, displayName: first.name, role: TeamRole.OWNER, joinedAt, version: 1 },
    { teamId: teamBetaId, userId: first.id, displayName: first.name, role: TeamRole.PILOT, joinedAt, version: 1 },
  ];
}

function resetState(): void {
  expireRpc = false;
  offlineRpc = false;
  weakFactor = false;
  nextCookie = defaultCookie;
  teamRecords = seedTeams();
  members = seedMembers();
  invitations = [];
}

function isSessionValue(value: string): value is SessionValue {
  return value in users;
}

function cookieSession(header: string | null | undefined): SessionValue | null {
  for (const part of (header ?? "").split(";")) {
    const [name, value] = part.trim().split("=");
    if (name === cookieName && value && isSessionValue(value)) return value;
  }
  return null;
}

function teams(userId: string) {
  return members
    .filter((member) => member.userId === userId)
    .flatMap((member) => {
      const team = teamRecords.find((candidate) => candidate.id === member.teamId);
      return team ? [create(TeamMembershipSchema, { team, role: member.role })] : [];
    });
}

function hasSession(req: IncomingMessage): boolean {
  return cookieSession(req.headers.cookie) !== null;
}

function sameOrigin(req: IncomingMessage): boolean {
  const origin = req.headers.origin;
  const site = req.headers["sec-fetch-site"];
  if (origin && origin !== appOrigin) {
    return false;
  }
  return !site || site === "same-origin" || site === "none";
}

function json(res: ServerResponse, status: number, body: unknown): void {
  const raw = JSON.stringify(body);
  res.writeHead(status, {
    "Content-Type": "application/json",
    "Cache-Control": "no-store",
    "Content-Length": Buffer.byteLength(raw),
  });
  res.end(raw);
}

function sessionCookie(maxAge: number, value: string = defaultCookie): string {
  return `${cookieName}=${value}; Path=/; HttpOnly; Secure; SameSite=Strict; Max-Age=${maxAge}`;
}

function safeReturnTo(value: string | null): string {
  if (!value || !value.startsWith("/") || value.startsWith("//") || value.startsWith("/\\")) {
    return "/teams";
  }
  if (value === "/auth" || value.startsWith("/auth/")) {
    return "/teams";
  }
  return value;
}

function escapeHTML(value: string): string {
  return value.replace(/[&<>"']/g, (char) => {
    switch (char) {
      case "&":
        return "&amp;";
      case "<":
        return "&lt;";
      case ">":
        return "&gt;";
      case '"':
        return "&quot;";
      default:
        return "&#39;";
    }
  });
}

function landing(res: ServerResponse, returnTo: string): void {
  const target = escapeHTML(returnTo);
  const body = `<!doctype html><html lang="pt-PT"><head><meta charset="utf-8"><meta http-equiv="refresh" content="0;url=${target}"><title>PitBox</title></head><body><a href="${target}">Continuar</a></body></html>`;
  res.writeHead(200, {
    "Content-Type": "text/html; charset=utf-8",
    "Cache-Control": "no-store",
    "Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'",
    "Referrer-Policy": "no-referrer",
    "Set-Cookie": sessionCookie(3600, nextCookie),
  });
  res.end(body);
}

function requireUser(context: { requestHeader: Headers }): (typeof users)[SessionValue] {
  const headers = Object.fromEntries(context.requestHeader) as Record<string, string>;
  const session = cookieSession(context.requestHeader.get("cookie"));
  if (!sameOrigin({ headers } as unknown as IncomingMessage) || !session) {
    throw new ConnectError("unauthenticated", Code.Unauthenticated);
  }
  if (expireRpc) throw new ConnectError("session revoked", Code.Unauthenticated);
  if (offlineRpc) throw new ConnectError("temporarily unavailable", Code.Unavailable);
  return users[session];
}

function roleOf(userId: string, teamId: string): TeamRole | null {
  return members.find((member) => member.userId === userId && member.teamId === teamId)?.role ?? null;
}

// Non-members and members without the right role both see NOT_FOUND, like the real API.
function requireOwner(userId: string, teamId: string): void {
  if (roleOf(userId, teamId) !== TeamRole.OWNER) throw new ConnectError("not found", Code.NotFound);
}

function toMember(member: MockMember) {
  return create(MemberSchema, {
    userId: member.userId,
    displayName: member.displayName,
    role: member.role,
    joinedAt: timestampFromDate(member.joinedAt),
    version: member.version,
  });
}

function toInvitation(invitation: MockInvitation) {
  return create(InvitationSchema, {
    id: invitation.id,
    role: invitation.role,
    createdAt: timestampFromDate(invitation.createdAt),
    expiresAt: timestampFromDate(new Date(invitation.createdAt.getTime() + 7 * 24 * 3600 * 1000)),
  });
}

const rpcHandler = connectNodeAdapter({
  routes(router) {
    router.service(TeamService, {
      listMyTeams(_request, context) {
        const user = requireUser(context);
        return create(ListMyTeamsResponseSchema, { teams: teams(user.id) });
      },
      createTeam(request, context) {
        const user = requireUser(context);
        if (weakFactor) throw new ConnectError("strong factor required", Code.Unauthenticated);
        const team = create(TeamSchema, {
          id: randomUUID(),
          name: request.name,
          category: request.category === "" ? "1/8 TT nitro" : request.category,
          countryCode: request.countryCode || "PT",
          ...(request.club ? { club: request.club } : {}),
        });
        teamRecords.push(team);
        members.push({
          teamId: team.id,
          userId: user.id,
          displayName: user.name,
          role: TeamRole.OWNER,
          joinedAt: new Date(),
          version: 1,
        });
        return create(CreateTeamResponseSchema, { team });
      },
    });
    router.service(MemberService, {
      listMembers(request, context) {
        const user = requireUser(context);
        const role = roleOf(user.id, request.teamId);
        if (role === null || role === TeamRole.GUEST) throw new ConnectError("not found", Code.NotFound);
        return create(ListMembersResponseSchema, {
          members: members.filter((member) => member.teamId === request.teamId).map(toMember),
        });
      },
      listInvitations(request, context) {
        const user = requireUser(context);
        requireOwner(user.id, request.teamId);
        return create(ListInvitationsResponseSchema, {
          invitations: invitations
            .filter((invitation) => invitation.teamId === request.teamId && invitation.state === "pending")
            .map(toInvitation),
        });
      },
      inviteMember(request, context) {
        const user = requireUser(context);
        requireOwner(user.id, request.teamId);
        const invitation: MockInvitation = {
          id: randomUUID(),
          teamId: request.teamId,
          role: request.role,
          token: randomBytes(32).toString("base64url"),
          createdAt: new Date(),
          state: "pending",
        };
        invitations.push(invitation);
        return create(InviteMemberResponseSchema, {
          invitation: toInvitation(invitation),
          token: invitation.token,
        });
      },
      acceptInvitation(request, context) {
        const user = requireUser(context);
        const invitation = invitations.find((candidate) => candidate.token === request.token);
        // One generic error for unknown, used and revoked links.
        if (!invitation || invitation.state !== "pending") throw new ConnectError("not found", Code.NotFound);
        const team = teamRecords.find((candidate) => candidate.id === invitation.teamId);
        if (!team) throw new ConnectError("not found", Code.NotFound);
        invitation.state = "used";
        if (roleOf(user.id, team.id) === null) {
          members.push({
            teamId: team.id,
            userId: user.id,
            displayName: user.name,
            role: invitation.role,
            joinedAt: new Date(),
            version: 1,
          });
        }
        return create(AcceptInvitationResponseSchema, {
          membership: create(TeamMembershipSchema, { team, role: invitation.role }),
        });
      },
      revokeInvitation(request, context) {
        const user = requireUser(context);
        requireOwner(user.id, request.teamId);
        const invitation = invitations.find(
          (candidate) => candidate.id === request.invitationId && candidate.teamId === request.teamId,
        );
        if (!invitation || invitation.state !== "pending") throw new ConnectError("not found", Code.NotFound);
        invitation.state = "revoked";
        return create(RevokeInvitationResponseSchema, {});
      },
      removeMember(request, context) {
        const user = requireUser(context);
        requireOwner(user.id, request.teamId);
        const index = members.findIndex(
          (member) => member.teamId === request.teamId && member.userId === request.userId,
        );
        const target = members[index];
        if (!target || target.role === TeamRole.OWNER) throw new ConnectError("not found", Code.NotFound);
        if (target.version !== request.version) throw new ConnectError("version conflict", Code.Aborted);
        members.splice(index, 1);
        return create(RemoveMemberResponseSchema, {});
      },
    });
  },
});

const server = createServer((req, res) => {
  const url = new URL(req.url ?? "/", appOrigin);

  if (url.pathname === "/healthz") {
    res.writeHead(200, { "Content-Type": "text/plain" });
    res.end("ok\n");
    return;
  }

  if (url.pathname === "/__test/expire-rpc" && req.method === "POST") {
    expireRpc = true;
    res.writeHead(204, { "Cache-Control": "no-store" });
    res.end();
    return;
  }

  if (url.pathname === "/__test/offline-rpc" && req.method === "POST") {
    offlineRpc = true;
    res.writeHead(204, { "Cache-Control": "no-store" });
    res.end();
    return;
  }

  if (url.pathname === "/__test/reset" && req.method === "POST") {
    resetState();
    res.writeHead(204, { "Cache-Control": "no-store" });
    res.end();
    return;
  }

  if (url.pathname === "/__test/next-user" && req.method === "POST") {
    const value = url.searchParams.get("session") ?? "";
    if (!isSessionValue(value)) {
      json(res, 400, { error: "unknown session" });
      return;
    }
    nextCookie = value;
    res.writeHead(204, { "Cache-Control": "no-store" });
    res.end();
    return;
  }

  if (url.pathname === "/__test/weak-factor" && req.method === "POST") {
    weakFactor = true;
    res.writeHead(204, { "Cache-Control": "no-store" });
    res.end();
    return;
  }

  if (url.pathname === "/auth/account-security") {
    res.writeHead(303, { Location: "/teams/new", "Cache-Control": "no-store", "Referrer-Policy": "no-referrer" });
    res.end();
    return;
  }

  if (url.pathname === "/auth/login") {
    expireRpc = false;
    offlineRpc = false;
    if (url.searchParams.get("step_up") === "true") weakFactor = false;
    const returnTo = safeReturnTo(url.searchParams.get("return_to"));
    res.writeHead(302, {
      Location: `/auth/callback?return_to=${encodeURIComponent(returnTo)}`,
      "Cache-Control": "no-store",
    });
    res.end();
    return;
  }

  if (url.pathname === "/auth/callback") {
    landing(res, safeReturnTo(url.searchParams.get("return_to")));
    return;
  }

  if (url.pathname === "/auth/session") {
    if (!sameOrigin(req)) {
      json(res, 403, { error: "cross-origin request refused" });
      return;
    }
    if (!hasSession(req)) {
      json(res, 401, { authenticated: false });
      return;
    }
    const session = cookieSession(req.headers.cookie) ?? defaultCookie;
    json(res, 200, {
      authenticated: true,
      user_id: users[session].id,
      amr: weakFactor ? ["pwd"] : ["pwd", "user"],
      auth_time: "2026-10-25T12:00:00Z",
    });
    return;
  }

  if (url.pathname === "/auth/logout") {
    if (req.method !== "POST" || req.headers.origin !== appOrigin) {
      json(res, 403, { error: "cross-origin request refused" });
      return;
    }
    // Same shape as the real BFF: a 200 + meta refresh, not a 303. The app CSP's
    // form-action 'self' would block a cross-origin redirect after the form POST.
    const target = "/auth/signed-out";
    res.writeHead(200, {
      "Content-Type": "text/html; charset=utf-8",
      "Cache-Control": "no-store",
      "Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'",
      "Set-Cookie": sessionCookie(0),
    });
    res.end(
      `<!doctype html><html lang="pt-PT"><head><meta charset="utf-8"><meta http-equiv="refresh" content="0;url=${target}"><title>PitBox</title></head><body><a href="${target}">Continuar</a></body></html>`,
    );
    return;
  }

  if (url.pathname === "/auth/signed-out") {
    res.writeHead(303, { Location: "/login?logged_out=1", "Cache-Control": "no-store" });
    res.end();
    return;
  }

  if (url.pathname.startsWith("/pitbox.")) {
    rpcHandler(req, res);
    return;
  }

  json(res, 404, { error: "not found" });
});

server.listen(port, "127.0.0.1", () => {
  process.stdout.write(`mock BFF on http://127.0.0.1:${port} (app origin ${appOrigin})\n`);
});
