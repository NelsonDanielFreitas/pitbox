import { expect, test, type APIRequestContext } from "@playwright/test";

import { state } from "./server";

const origin = "http://localhost:4174";
const pull = "/pitbox.sync.v1.SyncService/Pull";

async function pullAs(request: APIRequestContext, session: string | undefined, teamId: string) {
  return request.post(`${origin}${pull}`, {
    headers: {
      "Content-Type": "application/json",
      "Connect-Protocol-Version": "1",
      Origin: origin,
      ...(session ? { Cookie: `__Host-pitbox_session=${session}` } : {}),
    },
    data: { teamId, limit: 10 },
  });
}

test.describe("the real server refuses what it must", () => {
  test("no session: unauthenticated", async ({ request }) => {
    expect((await pullAs(request, undefined, state.teamId)).status()).toBe(401);
  });

  test("a member of another team gets not found, like a team that does not exist", async ({ request }) => {
    const foreign = await pullAs(request, state.outsider.session, state.teamId);
    const missing = await pullAs(request, state.outsider.session, "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4dee");
    expect(foreign.status()).toBe(404);
    expect(((await foreign.json()) as { code: string }).code).toBe("not_found");
    expect(await foreign.json()).toEqual(await missing.json());
  });

  test("a request from another origin is refused even with a valid session", async ({ request }) => {
    const response = await request.post(`${origin}${pull}`, {
      headers: {
        "Content-Type": "application/json",
        "Connect-Protocol-Version": "1",
        Origin: "https://evil.example",
        Cookie: `__Host-pitbox_session=${state.owner.session}`,
      },
      data: { teamId: state.teamId, limit: 10 },
    });
    expect(response.status()).toBe(403);
  });

  test("a member can pull", async ({ request }) => {
    expect((await pullAs(request, state.pilot.session, state.teamId)).status()).toBe(200);
  });
});
