import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { connectNodeAdapter } from "@connectrpc/connect-node";
import { createServer, type IncomingMessage, type ServerResponse } from "node:http";

import {
  ListMyTeamsResponseSchema,
  TeamMembershipSchema,
  TeamRole,
  TeamSchema,
  TeamService,
} from "@/gen/pitbox/identity/v1/team_service_pb";

const port = Number(process.env.PITBOX_E2E_BFF_PORT ?? 4010);
const appOrigin = process.env.PITBOX_E2E_ORIGIN ?? "http://localhost:4173";
const cookieName = "__Host-pitbox_session";
const cookieValue = "mock-session";
const userId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60";
let expireRpc = false;

function teams() {
  return [
    create(TeamMembershipSchema, {
      team: create(TeamSchema, {
        id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d61",
        name: "Equipa Alpha",
        category: "1/8 TT nitro",
        countryCode: "PT",
      }),
      role: TeamRole.OWNER,
    }),
    create(TeamMembershipSchema, {
      team: create(TeamSchema, {
        id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d62",
        name: "Equipa Beta",
        category: "1/8 TT nitro",
        club: "Clube de Braga",
        countryCode: "PT",
      }),
      role: TeamRole.PILOT,
    }),
  ];
}

function hasSession(req: IncomingMessage): boolean {
  return (req.headers.cookie ?? "").split(";").some((part) => part.trim() === `${cookieName}=${cookieValue}`);
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

function sessionCookie(maxAge: number): string {
  return `${cookieName}=${cookieValue}; Path=/; HttpOnly; Secure; SameSite=Strict; Max-Age=${maxAge}`;
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
    "Set-Cookie": sessionCookie(3600),
  });
  res.end(body);
}

const rpcHandler = connectNodeAdapter({
  routes(router) {
    router.service(TeamService, {
      listMyTeams(_request, context) {
        if (!sameOrigin({ headers: Object.fromEntries(context.requestHeader) } as IncomingMessage) ||
            !context.requestHeader.get("cookie")?.includes(`${cookieName}=${cookieValue}`)) {
          throw new ConnectError("unauthenticated", Code.Unauthenticated);
        }
        if (expireRpc) {
          throw new ConnectError("session revoked", Code.Unauthenticated);
        }
        return create(ListMyTeamsResponseSchema, { teams: teams() });
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

  if (url.pathname === "/auth/login") {
    expireRpc = false;
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
    json(res, 200, {
      authenticated: true,
      user_id: userId,
      amr: [],
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
