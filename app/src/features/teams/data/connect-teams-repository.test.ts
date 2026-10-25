import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { describe, expect, it, vi } from "vitest";

import {
  ListMyTeamsResponseSchema,
  TeamMembershipSchema,
  TeamRole,
  TeamSchema,
} from "@/gen/pitbox/identity/v1/team_service_pb";

import {
  ConnectTeamsRepository,
  isUnauthenticated,
  roleFromProto,
} from "./connect-teams-repository";

function membership(name: string, role: TeamRole) {
  return create(TeamMembershipSchema, {
    team: create(TeamSchema, {
      id: name === "Alpha" ? "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60" : "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d61",
      name,
      category: "1/8 TT nitro",
      countryCode: "PT",
    }),
    role,
  });
}

function listResponse(teams: ReturnType<typeof membership>[], nextPageToken = "") {
  return create(ListMyTeamsResponseSchema, { teams, nextPageToken });
}

describe("roleFromProto", () => {
  it("maps every role", () => {
    expect(roleFromProto(TeamRole.OWNER)).toBe("owner");
    expect(roleFromProto(TeamRole.MECHANIC)).toBe("mechanic");
    expect(roleFromProto(TeamRole.PILOT)).toBe("pilot");
    expect(roleFromProto(TeamRole.GUEST)).toBe("guest");
    expect(roleFromProto(TeamRole.UNSPECIFIED)).toBe("unknown");
  });
});

describe("ConnectTeamsRepository", () => {
  it("walks the cursor to the end and returns domain teams", async () => {
    const list = vi
      .fn()
      .mockResolvedValueOnce(listResponse([membership("Alpha", TeamRole.OWNER)], "next"))
      .mockResolvedValueOnce(listResponse([membership("Beta", TeamRole.PILOT)]));
    const repository = new ConnectTeamsRepository({ listMyTeams: list });

    const teams = await repository.listMyTeams();

    expect(teams).toHaveLength(2);
    expect(teams[0]).toMatchObject({ name: "Alpha", role: "owner" });
    expect(teams[1]).toMatchObject({ name: "Beta", role: "pilot" });
    expect(list).toHaveBeenNthCalledWith(1, { pageSize: 200, pageToken: "" });
    expect(list).toHaveBeenNthCalledWith(2, { pageSize: 200, pageToken: "next" });
  });

  it("stops after five pages", async () => {
    const list = vi.fn().mockResolvedValue(listResponse([], "more"));
    const repository = new ConnectTeamsRepository({ listMyTeams: list });

    await expect(repository.listMyTeams()).resolves.toEqual([]);
    expect(list).toHaveBeenCalledTimes(5);
  });

  it("detects unauthenticated Connect errors", () => {
    expect(isUnauthenticated(new ConnectError("no session", Code.Unauthenticated))).toBe(true);
    expect(isUnauthenticated(new ConnectError("other", Code.Internal))).toBe(false);
  });
});
