import { describe, expect, it, vi } from "vitest";

import type { Team } from "../domain/team";
import { AppTeamsRepository } from "./team-commands-repository";

const team: Team = {
  id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60",
  name: "Freitas Racing",
  category: "1/8 TT nitro",
  countryCode: "PT",
  role: "owner",
};

describe("AppTeamsRepository", () => {
  it("reads locally and refreshes all local teams only after server creation succeeds", async () => {
    const local = { listMyTeams: vi.fn().mockResolvedValue([team]) };
    const remote = { createTeam: vi.fn().mockResolvedValue(team) };
    const refresh = vi.fn().mockResolvedValue(undefined);
    const repository = new AppTeamsRepository(local, remote, refresh);

    await expect(repository.listMyTeams()).resolves.toEqual([team]);
    await expect(repository.createTeam({
      requestId: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d61",
      name: team.name,
      category: "",
      countryCode: "PT",
    })).resolves.toEqual(team);

    expect(remote.createTeam).toHaveBeenCalledTimes(1);
    expect(refresh).toHaveBeenCalledTimes(1);
  });

  it("does not refresh local data when the server rejects team creation", async () => {
    const remote = { createTeam: vi.fn().mockRejectedValue(new Error("denied")) };
    const refresh = vi.fn();
    const repository = new AppTeamsRepository({ listMyTeams: vi.fn() }, remote, refresh);

    await expect(repository.createTeam({
      requestId: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d61",
      name: team.name,
      category: "",
      countryCode: "PT",
    })).rejects.toThrow("denied");
    expect(refresh).not.toHaveBeenCalled();
  });
});
