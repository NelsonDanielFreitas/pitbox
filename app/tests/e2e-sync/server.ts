import { readFileSync } from "node:fs";
import path from "node:path";

import type { BrowserContext } from "@playwright/test";

/** What scripts/e2e-server.sh wrote: synthetic people, their sessions and the control port. */
export interface E2EState {
  teamId: string;
  otherTeamId: string;
  owner: { id: string; session: string };
  pilot: { id: string; session: string };
  outsider: { id: string; session: string };
  controlUrl: string;
  controlToken: string;
}

export type Actor = "owner" | "pilot" | "outsider";

export const state: E2EState = JSON.parse(
  readFileSync(path.resolve(import.meta.dirname, "../../.e2e/state.json"), "utf8"),
) as E2EState;

/** Logs a browser context in the way the BFF would: the __Host- cookie, Secure, Path=/, no Domain. */
export async function signInAs(context: BrowserContext, actor: Actor): Promise<void> {
  await context.addCookies([
    {
      name: "__Host-pitbox_session",
      value: state[actor].session,
      // domain + path, not url: Chromium refuses a __Host- cookie given as a http URL. A domain
      // without a leading dot is still a host-only cookie.
      domain: "localhost",
      path: "/",
      httpOnly: true,
      secure: true,
      sameSite: "Strict",
    },
  ]);
}

type Row = Record<string, unknown>;

/** Reads one of the control port's fixed views of the server's data. */
export async function serverRows(name: string): Promise<Row[]> {
  const response = await fetch(`${state.controlUrl}/data/${name}`, {
    headers: { Authorization: `Bearer ${state.controlToken}` },
  });
  if (!response.ok) throw new Error(`control port ${name}: ${response.status}`);
  return (await response.json()) as Row[];
}
